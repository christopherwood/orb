/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package bdd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cucumber/godog"
	"github.com/mr-tron/base58"
	"github.com/sirupsen/logrus"

	"github.com/hyperledger/aries-framework-go/component/storageutil/mem"
	"github.com/hyperledger/aries-framework-go/pkg/doc/ld"
	ldstore "github.com/hyperledger/aries-framework-go/pkg/store/ld"
	"github.com/trustbloc/sidetree-go/pkg/canonicalizer"
	"github.com/trustbloc/sidetree-go/pkg/commitment"
	"github.com/trustbloc/sidetree-go/pkg/document"
	"github.com/trustbloc/sidetree-go/pkg/docutil"
	"github.com/trustbloc/sidetree-go/pkg/encoder"
	"github.com/trustbloc/sidetree-go/pkg/hashing"
	"github.com/trustbloc/sidetree-go/pkg/patch"
	"github.com/trustbloc/sidetree-go/pkg/util/ecsigner"
	"github.com/trustbloc/sidetree-go/pkg/util/pubkey"
	"github.com/trustbloc/sidetree-go/pkg/versions/1_0/client"
	"github.com/trustbloc/sidetree-go/pkg/versions/1_0/model"

	"github.com/trustbloc/orb/internal/pkg/ldcontext"
	"github.com/trustbloc/orb/pkg/cas/ipfs"
	"github.com/trustbloc/orb/pkg/discovery/endpoint/restapi"
	"github.com/trustbloc/orb/pkg/mocks"
	"github.com/trustbloc/orb/pkg/orbclient/aoprovider"
	"github.com/trustbloc/orb/pkg/orbclient/doctransformer"
	"github.com/trustbloc/orb/pkg/orbclient/resolutionverifier"
)

var logger = logrus.New()

const (
	didDocNamespace = "did:orb"

	initialStateSeparator = ":"

	sha2_256 = 18

	anchorTimeDelta = 300

	testCID = "bafkreie4a6z6hosibbfz2dd7vhsukojw3gwmldh5jvy5uh3mjfrubiq5hi"
)

const addPublicKeysTemplate = `[
	{
      "id": "%s",
      "purposes": ["authentication"],
      "type": "JsonWebKey2020",
      "publicKeyJwk": {
        "kty": "EC",
        "crv": "P-256K",
        "x": "PUymIqdtF_qxaAqPABSw-C-owT1KYYQbsMKFM-L9fJA",
        "y": "nM84jDHCMOTGTh_ZdHq4dBBdo4Z5PkEOW9jA8z8IsGc"
      }
    }
  ]`

const removePublicKeysTemplate = `["%s"]`

const addServicesTemplate = `[
    {
      "id": "%s",
      "type": "SecureDataStore",
      "serviceEndpoint": "http://hub.my-personal-server.com"
    }
  ]`

const removeServicesTemplate = `["%s"]`

const docTemplate = `{
  "alsoKnownAs": ["https://myblog.example/"],
  "publicKey": [
   {
     "id": "%s",
     "type": "JsonWebKey2020",
     "purposes": ["authentication"],
     "publicKeyJwk": %s
   },
   {
     "id": "auth",
     "type": "Ed25519VerificationKey2018",
     "purposes": ["assertionMethod"],
     "publicKeyJwk": %s
   }
  ],
  "service": [
	{
	   "id": "didcomm",
	   "type": "did-communication",
	   "recipientKeys": ["%s"],
	   "serviceEndpoint": "https://hub.example.com/.identity/did:example:0123456789abcdef/",
	   "priority": 0
	}
  ]
}`

// DIDOrbSteps
type DIDOrbSteps struct {
	state *state

	namespace                string
	createRequest            *model.CreateRequest
	recoveryKeys             []*ecdsa.PrivateKey
	updateKeys               []*ecdsa.PrivateKey
	resp                     *httpResponse
	resolutionResult         *document.ResolutionResult
	bddContext               *BDDContext
	interimDID               string
	prevCanonicalDID         string
	canonicalDID             string
	prevEquivalentDID        []string
	equivalentDID            []string
	retryDID                 string
	resolutionEndpoint       string
	operationEndpoint        string
	sidetreeURL              string
	createResponses          *responses[*createDIDResponse]
	createAndUpdateResponses *createAndUpdateResponses
	updateResponses          []*updateDIDResponse
	httpClient               *httpClient
	didPrintEnabled          bool
}

type didResponse interface {
	DID() string
}

type responses[T didResponse] struct {
	responses []T
	mutex     sync.RWMutex
}

func newResponses[T didResponse]() *responses[T] {
	return &responses[T]{}
}

func (r *responses[T]) Add(response T) {
	r.mutex.Lock()
	r.responses = append(r.responses, response)
	r.mutex.Unlock()
}

func (r *responses[T]) Clear() {
	r.mutex.Lock()
	r.responses = nil
	r.mutex.Unlock()
}

func (r *responses[T]) GetAll() []T {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return r.responses
}

func (r *responses[T]) Get(ctx context.Context, n int) ([]T, error) {
	for {
		select {
		case <-time.After(100 * time.Millisecond):
			r := r.GetAll()
			if len(r) >= n {
				return r[0:n], nil
			}

		case <-ctx.Done():
			return nil, fmt.Errorf("wait for responses: %w", ctx.Err())
		}
	}
}

type createAndUpdateResponses struct {
	*responses[*createAndUpdateDIDResponse]
}

func newCreateAndUpdateResponses() *createAndUpdateResponses {
	return &createAndUpdateResponses{newResponses[*createAndUpdateDIDResponse]()}
}

func (r *createAndUpdateResponses) UpdateResponses() []*updateDIDResponse {
	responses := make([]*updateDIDResponse, len(r.responses.GetAll()))

	for i, r := range r.responses.GetAll() {
		responses[i] = r.updateDIDResponse
	}

	return responses
}

// NewDIDSideSteps
func NewDIDSideSteps(context *BDDContext, state *state, httpClient *httpClient, namespace string) *DIDOrbSteps {
	return &DIDOrbSteps{
		bddContext:               context,
		state:                    state,
		namespace:                namespace,
		httpClient:               httpClient,
		didPrintEnabled:          true,
		createResponses:          newResponses[*createDIDResponse](),
		createAndUpdateResponses: newCreateAndUpdateResponses(),
	}
}

func (d *DIDOrbSteps) discoverEndpoints() error {
	resp, err := d.httpClient.Get("https://localhost:48326/.well-known/did-orb")
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received status code %d: %s", resp.StatusCode, resp.ErrorMsg)
	}

	var w restapi.WellKnownResponse
	if err := json.Unmarshal(resp.Payload, &w); err != nil {
		return err
	}

	resp, err = d.httpClient.Get(
		fmt.Sprintf("https://localhost:48326/.well-known/webfinger?resource=%s",
			url.PathEscape(w.ResolutionEndpoint)))
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("received status code %d: %s", resp.StatusCode, resp.ErrorMsg)
	}

	var webFingerResponse restapi.JRD
	if err := json.Unmarshal(resp.Payload, &webFingerResponse); err != nil {
		return err
	}

	d.resolutionEndpoint = strings.ReplaceAll(webFingerResponse.Links[0].Href, "orb.domain1.com", "localhost:48326")

	resp, err = d.httpClient.Get(
		fmt.Sprintf("https://localhost:48326/.well-known/webfinger?resource=%s",
			url.PathEscape(w.OperationEndpoint)))
	if err != nil {
		return err
	}

	if err := json.Unmarshal(resp.Payload, &webFingerResponse); err != nil {
		return err
	}

	d.operationEndpoint = strings.ReplaceAll(webFingerResponse.Links[0].Href, "orb.domain1.com", "localhost:48326")

	return nil
}

type provider struct {
	ContextStore        ldstore.ContextStore
	RemoteProviderStore ldstore.RemoteProviderStore
}

func (p *provider) JSONLDContextStore() ldstore.ContextStore {
	return p.ContextStore
}

func (p *provider) JSONLDRemoteProviderStore() ldstore.RemoteProviderStore {
	return p.RemoteProviderStore
}

func (d *DIDOrbSteps) clientRequestsAnchorOrigin(url string) error {
	logger.Info("requesting anchor origin (client)")

	if os.Getenv("CAS_TYPE") != "ipfs" {
		logger.Info("ignoring 'request anchor origin' test case since cas type is NOT set to 'ifps'")
		return nil
	}

	cid, suffix, err := extractCIDAndSuffix(d.canonicalDID)
	if err != nil {
		return err
	}

	contextStore, err := ldstore.NewContextStore(mem.NewProvider())
	if err != nil {
		return fmt.Errorf("create JSON-LD context store: %w", err)
	}

	remoteProviderStore, err := ldstore.NewRemoteProviderStore(mem.NewProvider())
	if err != nil {
		return fmt.Errorf("create remote provider store: %w", err)
	}

	p := &provider{
		ContextStore:        contextStore,
		RemoteProviderStore: remoteProviderStore,
	}

	docLoader, err := ld.NewDocumentLoader(p, ld.WithExtraContexts(ldcontext.MustGetAll()...))

	casClient := ipfs.New(url, 20*time.Second, 0, &mocks.MetricsProvider{})

	orbClient, err := aoprovider.New(didDocNamespace, casClient,
		aoprovider.WithJSONLDDocumentLoader(docLoader),
		aoprovider.WithDisableProofCheck(true))
	if err != nil {
		return err
	}

	anchorOriginObj, err := orbClient.GetAnchorOrigin(cid, suffix)
	if err != nil {
		return err
	}

	anchorOrigin, ok := anchorOriginObj.(string)
	if !ok {
		return fmt.Errorf("unexpected interface '%T' for anchor origin object", anchorOriginObj)
	}

	logger.Infof("got anchor origin: %s", anchorOrigin)

	expectedOrigin := "https://orb.domain1.com"
	if anchorOrigin != expectedOrigin {
		return fmt.Errorf("anchor origin: expected %s, got %s", expectedOrigin, anchorOrigin)
	}

	return err
}

func (d *DIDOrbSteps) clientVerifiesResolvedDocument() error {
	versions := []string{"1.0"}

	if os.Getenv("VERSION_TEST") == "true" {
		versions = append(versions, "test")
	}

	logger.Infof("verify resolved document (client) with versions: %s", versions)

	verifier, err := resolutionverifier.New("did:orb", resolutionverifier.WithProtocolVersions(versions))
	if err != nil {
		return err
	}

	return verifier.Verify(d.resolutionResult)
}

func (d *DIDOrbSteps) clientVerifiesWebDocumentFromOrbDocument(didWebVar, didOrbVar string) error {
	logger.Info("verify that resolved web document is produced from orb resolution result")

	didWebResolutionResultStr, ok := d.state.getVar(didWebVar)
	if !ok {
		return fmt.Errorf("missing did:web resolution result from var[%s]", didWebVar)
	}

	var didWebResolutionResult document.ResolutionResult
	err := json.Unmarshal([]byte(didWebResolutionResultStr), &didWebResolutionResult)
	if err != nil {
		return err
	}

	didOrbResolutionResultStr, ok := d.state.getVar(didOrbVar)
	if !ok {
		return fmt.Errorf("missing did:orb resolution result from var[%s]", didOrbVar)
	}

	var didOrbResolutionResult document.ResolutionResult
	err = json.Unmarshal([]byte(didOrbResolutionResultStr), &didOrbResolutionResult)
	if err != nil {
		return err
	}

	return diddoctransformer.VerifyWebDocumentFromOrbDocument(&didWebResolutionResult, &didOrbResolutionResult)
}

func (d *DIDOrbSteps) clientFailsToVerifyResolvedDocument() error {
	logger.Info("fail to verify resolved document (mis-configured client)")

	verifier, err := resolutionverifier.New("did:orb", resolutionverifier.WithEnableBase(true))
	if err != nil {
		return err
	}

	err = verifier.Verify(d.resolutionResult)
	if err == nil {
		return fmt.Errorf("should have failed to verify document with mis-configured client")
	}

	if strings.Contains(err.Error(), "documents don't match") {
		// expected error since document rendering is different with and without base
		return nil
	}

	return fmt.Errorf("unexpected error for mis-configured client: %w", err)
}

func extractCIDAndSuffix(canonicalID string) (string, string, error) {
	parts := strings.Split(canonicalID, ":")

	if len(parts) != 4 {
		return "", "", fmt.Errorf("expected 4 got %d parts for canonical ID", len(parts))
	}

	return parts[2], parts[3], nil
}

func (d *DIDOrbSteps) createDIDDocumentSaveIDToVar(url, varName string) error {
	if err := d.createDIDDocument(url); err != nil {
		return err
	}

	d.state.setVar(varName, d.interimDID)

	return nil
}

func (d *DIDOrbSteps) createDIDDocumentSaveSuffixToVar(url, varName string) error {
	if err := d.createDIDDocument(url); err != nil {
		return err
	}

	parts := strings.Split(d.interimDID, docutil.NamespaceDelimiter)

	const minParts = 4
	if len(parts) < minParts {
		return fmt.Errorf("id[%s] has to have at least four parts", d.interimDID)
	}

	// suffix is always the last part
	suffix := parts[len(parts)-1]

	d.state.setVar(varName, suffix)

	return nil
}

func (d *DIDOrbSteps) resetKeysToLastSuccessful(url string) error {
	err := d.resolveDIDDocumentWithID(url, d.canonicalDID)
	if err != nil {
		return err
	}

	var result document.ResolutionResult
	err = json.Unmarshal(d.resp.Payload, &result)
	if err != nil {
		return err
	}

	metadataObj, ok := result.DocumentMetadata["method"]
	if !ok {
		return fmt.Errorf("missing method")
	}

	metadataMap, ok := metadataObj.(map[string]interface{})
	if !ok {
		return fmt.Errorf("method is wrong type")
	}

	updateCommitment := metadataMap[document.UpdateCommitmentProperty].(string)
	recoveryCommitment := metadataMap[document.RecoveryCommitmentProperty].(string)

	d.updateKeys, err = removeKeysAfterCommitment(d.updateKeys, updateCommitment)
	if err != nil {
		return err
	}

	d.recoveryKeys, err = removeKeysAfterCommitment(d.recoveryKeys, recoveryCommitment)
	if err != nil {
		return err
	}

	return nil
}

func removeKeysAfterCommitment(keys []*ecdsa.PrivateKey, cmt string) ([]*ecdsa.PrivateKey, error) {
	for index, key := range keys {
		pubKey, err := pubkey.GetPublicKeyJWK(&key.PublicKey)
		if err != nil {
			return nil, err
		}

		c, err := commitment.GetCommitment(pubKey, sha2_256)
		if err != nil {
			return nil, err
		}

		if c == cmt {
			logger.Infof("found key index '%d' of %d keys that corresponds to last successful commitment '%s'", index, len(keys), cmt)
			return keys[:index+1], nil
		}
	}

	return keys, nil
}

func (d *DIDOrbSteps) createDIDDocument(url string) error {
	logger.Info("create did document")

	d.recoveryKeys = nil
	d.updateKeys = nil

	err := d.setSidetreeURL(url)
	if err != nil {
		return err
	}

	opaqueDoc, err := getOpaqueDocument("createKey")
	if err != nil {
		return err
	}

	recoveryKey, updateKey, reqBytes, err := getCreateRequest(d.sidetreeURL, opaqueDoc, nil, d.state)
	if err != nil {
		return err
	}

	d.recoveryKeys = append(d.recoveryKeys, recoveryKey)
	d.updateKeys = append(d.updateKeys, updateKey)

	d.resp, err = d.httpClient.Post(d.sidetreeURL, reqBytes, "application/json")
	if err == nil && d.resp.StatusCode == http.StatusOK {
		var req model.CreateRequest
		e := json.Unmarshal(reqBytes, &req)
		if e != nil {
			return e
		}

		var result document.ResolutionResult
		err = json.Unmarshal(d.resp.Payload, &result)
		if err != nil {
			return err
		}

		d.prettyPrint(&result)

		d.createRequest = &req
		d.interimDID = result.Document["id"].(string)
		d.bddContext.createdDID = result.Document["id"].(string)
		d.equivalentDID = document.StringArray(result.DocumentMetadata["equivalentId"])

		d.state.setResponse(string(d.resp.Payload))
	}

	return err
}

func (d *DIDOrbSteps) setSidetreeURL(url string) error {
	d.sidetreeURL = url

	return nil
}

func (d *DIDOrbSteps) updateDIDDocument(url string, patches []patch.Patch) error {
	err := d.setSidetreeURL(url)
	if err != nil {
		return err
	}

	uniqueSuffix, err := d.getUniqueSuffix()
	if err != nil {
		return err
	}

	logger.Infof("update did document: %s", uniqueSuffix)

	req, updateKey, err := d.getUpdateRequest(uniqueSuffix, patches)
	if err != nil {
		return err
	}

	d.resp, err = d.httpClient.Post(d.sidetreeURL, req, "application/json")
	if err == nil && d.resp.StatusCode == http.StatusOK {
		// update update key for subsequent update requests
		d.updateKeys = append(d.updateKeys, updateKey)
	}

	return err
}

func (d *DIDOrbSteps) deactivateDIDDocument(url string) error {
	err := d.setSidetreeURL(url)
	if err != nil {
		return err
	}

	uniqueSuffix, err := d.getUniqueSuffix()
	if err != nil {
		return err
	}

	logger.Infof("deactivate did document: %s", uniqueSuffix)

	req, err := d.getDeactivateRequest(uniqueSuffix)
	if err != nil {
		return err
	}

	d.resp, err = d.httpClient.Post(d.sidetreeURL, req, "application/json")
	return err
}

func (d *DIDOrbSteps) recoverDIDDocument(url string) error {
	err := d.setSidetreeURL(url)
	if err != nil {
		return err
	}

	uniqueSuffix, err := d.getUniqueSuffix()
	if err != nil {
		return err
	}

	logger.Infof("recover did document")

	opaqueDoc, err := getOpaqueDocument("recoveryKey")
	if err != nil {
		return err
	}

	req, recoveryKey, updateKey, err := d.getRecoverRequest(opaqueDoc, nil, uniqueSuffix)
	if err != nil {
		return err
	}

	d.resp, err = d.httpClient.Post(d.sidetreeURL, req, "application/json")
	if err == nil && d.resp.StatusCode == http.StatusOK {
		// update recovery and update key for subsequent requests
		d.recoveryKeys = append(d.recoveryKeys, recoveryKey)
		d.updateKeys = append(d.updateKeys, updateKey)
	}

	return err
}

func (d *DIDOrbSteps) addNPublicKeysToDIDDocument(url string, n int) error {
	var patches []patch.Patch

	for i := 1; i <= n; i++ {
		p, err := getAddPublicKeysPatch("key-" + strconv.Itoa(i))
		if err != nil {
			return err
		}

		patches = append(patches, p)
	}

	return d.updateDIDDocument(url, patches)
}

func (d *DIDOrbSteps) addPublicKeyToDIDDocument(url, keyID string) error {
	p, err := getAddPublicKeysPatch(keyID)
	if err != nil {
		return err
	}

	return d.updateDIDDocument(url, []patch.Patch{p})
}

func (d *DIDOrbSteps) removePublicKeyFromDIDDocument(url, keyID string) error {
	p, err := getRemovePublicKeysPatch(keyID)
	if err != nil {
		return err
	}

	return d.updateDIDDocument(url, []patch.Patch{p})
}

func (d *DIDOrbSteps) addServiceEndpointToDIDDocument(url, keyID string) error {
	p, err := getAddServiceEndpointsPatch(keyID)
	if err != nil {
		return err
	}

	return d.updateDIDDocument(url, []patch.Patch{p})
}

func (d *DIDOrbSteps) removeServiceEndpointsFromDIDDocument(url, keyID string) error {
	p, err := getRemoveServiceEndpointsPatch(keyID)
	if err != nil {
		return err
	}

	return d.updateDIDDocument(url, []patch.Patch{p})
}

func (d *DIDOrbSteps) addAlsoKnownAsURIToDIDDocument(url, uri string) error {
	resolved, err := d.state.resolveVars(uri)
	if err != nil {
		return err
	}

	p, err := getAddAlsoKnownAsPatch(resolved.(string))
	if err != nil {
		return err
	}

	return d.updateDIDDocument(url, []patch.Patch{p})
}

func (d *DIDOrbSteps) removeAlsoKnownAsURIFromDIDDocument(url, uri string) error {
	p, err := getRemoveAlsoKnownAsPatch(uri)
	if err != nil {
		return err
	}

	return d.updateDIDDocument(url, []patch.Patch{p})
}

func (d *DIDOrbSteps) checkErrorResp(errorMsg string) error {
	if !strings.Contains(d.resp.ErrorMsg, errorMsg) {
		return fmt.Errorf(`error resp "%s" doesn't contain "%s" status: %d`, d.resp.ErrorMsg, errorMsg, d.resp.StatusCode)
	}
	return nil
}

func (d *DIDOrbSteps) checkSuccessRespContains(msg string) error {
	return d.checkSuccessResp(msg, true)
}

func (d *DIDOrbSteps) checkSuccessRespDoesntContain(msg string) error {
	return d.checkSuccessResp(msg, false)
}

func (d *DIDOrbSteps) checkSuccessResp(msg string, contains bool) error {
	var err error

	const maxRetries = 10

	initialSleep := 350 // in milliseconds

	for i := 1; i <= maxRetries; i++ {
		err = d.checkSuccessRespHelper(msg, contains)
		if err == nil {
			return nil
		}

		if !strings.Contains(d.sidetreeURL, "identifiers") {
			// retries are for resolution only
			return err
		}

		if i <= 5 {
			time.Sleep(time.Duration(initialSleep) * time.Millisecond)
		} else {
			time.Sleep(time.Duration(i*initialSleep) * time.Millisecond)
		}

		logger.Infof("retrying check success response - attempt %d", i)

		resolveErr := d.resolveDIDDocumentWithID(d.sidetreeURL, d.retryDID)
		if resolveErr != nil {
			return resolveErr
		}
	}

	return err
}

func (d *DIDOrbSteps) checkSuccessRespHelper(msg string, contains bool) error {
	if d.resp.ErrorMsg != "" {
		return fmt.Errorf("error resp %s", d.resp.ErrorMsg)
	}

	if msg == "#interimDID" || msg == "#canonicalDID" || msg == "#emptydoc" {

		msg = strings.Replace(msg, "#canonicalDID", d.canonicalDID, -1)
		msg = strings.Replace(msg, "#interimDID", d.interimDID, -1)

		var result document.ResolutionResult
		err := json.Unmarshal(d.resp.Payload, &result)
		if err != nil {
			return err
		}

		didDoc := document.DidDocumentFromJSONLDObject(result.Document)

		// perform basic checks on document
		if didDoc.ID() == "" || didDoc.Context()[0] != "https://www.w3.org/ns/did/v1" ||
			(len(didDoc.PublicKeys()) > 0 && !strings.Contains(didDoc.PublicKeys()[0].Controller(), didDoc.ID())) {
			return fmt.Errorf("response is not a valid did document")
		}

		if msg == "#emptydoc" {
			if len(didDoc) > 2 { // has id and context
				return fmt.Errorf("response is not an empty document")
			}

			logger.Info("response contains empty did document")

			return nil
		}

		logger.Infof("response is a valid did document")
	}

	action := " "
	if !contains {
		action = " NOT"
	}

	if contains && !strings.Contains(string(d.resp.Payload), msg) {
		return fmt.Errorf("success resp doesn't contain %s", msg)
	}

	if !contains && strings.Contains(string(d.resp.Payload), msg) {
		return fmt.Errorf("success resp should NOT contain %s", msg)
	}

	logger.Infof("passed check that success response MUST%s contain %s", action, msg)

	return nil
}

func (d *DIDOrbSteps) checkResponseIsSuccess() error {
	if d.resp.ErrorMsg != "" {
		return fmt.Errorf("error resp %s", d.resp.ErrorMsg)
	}

	return nil
}

func (d *DIDOrbSteps) getPayloadForRequest(url string) ([]byte, error) {
	logger.Infof("sending request: %s", url)

	resp, err := d.httpClient.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status[%d], msg: %s", resp.StatusCode, resp.ErrorMsg)
	}

	return resp.Payload, nil
}

func (d *DIDOrbSteps) resolveDIDDocumentWithID(url, did string) error {
	err := d.setSidetreeURL(url)
	if err != nil {
		return err
	}

	d.resp, err = d.httpClient.Get(d.sidetreeURL + "/" + did)

	logger.Infof("sending request: %s", d.sidetreeURL+"/"+did)

	if err == nil && d.resp.Payload != nil {
		var result document.ResolutionResult
		err = json.Unmarshal(d.resp.Payload, &result)
		if err != nil {
			return err
		}

		d.resolutionResult = &result

		err = d.prettyPrint(&result)
		if err != nil {
			return err
		}

		canonicalIDEntry, ok := result.DocumentMetadata["canonicalId"]
		if ok {
			d.prevCanonicalDID = d.canonicalDID
			d.canonicalDID = canonicalIDEntry.(string)
		}

		equivalentIDEntry, ok := result.DocumentMetadata["equivalentId"]
		if ok {
			d.prevEquivalentDID = d.equivalentDID
			d.equivalentDID = document.StringArray(equivalentIDEntry)
		}

		d.state.setResponse(string(d.resp.Payload))
	}

	return err
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}

	return false
}

func (d *DIDOrbSteps) resolveDIDDocumentWithInterimDID(url string) error {
	logger.Infof("resolving did document with did: %s", d.interimDID)

	d.retryDID = d.interimDID

	return d.resolveDIDDocumentWithID(url, d.interimDID)
}

func (d *DIDOrbSteps) resolveDIDDocumentWithCanonicalDID(url string) error {
	logger.Infof("resolving did document with canonical did: %s", d.canonicalDID)

	d.retryDID = d.canonicalDID

	return d.resolveDIDDocumentWithID(url, d.canonicalDID)
}

func (d *DIDOrbSteps) resolveDIDDocumentWithCanonicalDIDAndVersionID(url, versionID string) error {
	logger.Infof("resolving did document with canonical did %s and version ID %s", d.canonicalDID, versionID)

	if err := d.state.resolveVarsInExpression(&versionID); err != nil {
		return err
	}

	didWithParam := d.canonicalDID + "?versionId=" + versionID

	d.retryDID = didWithParam

	return d.resolveDIDDocumentWithID(url, didWithParam)
}

func (d *DIDOrbSteps) resolveDIDDocumentWithCanonicalDIDAndVersionTime(url, versionTime string) error {
	logger.Infof("resolving did document with canonical did %s and version time %s", d.canonicalDID, versionTime)

	if err := d.state.resolveVarsInExpression(&versionTime); err != nil {
		return err
	}

	didWithParam := d.canonicalDID + "?versionTime=" + versionTime

	d.retryDID = didWithParam

	return d.resolveDIDDocumentWithID(url, didWithParam)
}

func (d *DIDOrbSteps) resolveDIDDocumentWithHint(url, hint string) error {
	cannonicalDIDParts := strings.SplitAfter(d.canonicalDID, d.namespace)

	didWithHint := cannonicalDIDParts[0] + ":" + hint + cannonicalDIDParts[1]

	logger.Infof("resolving did with hint: %s", didWithHint)

	d.retryDID = didWithHint

	return d.resolveDIDDocumentWithID(url, didWithHint)
}

func (d *DIDOrbSteps) resolveInterimDIDDocumentWithHint(url, hint string) error {
	interimDIDParts := strings.SplitAfter(d.interimDID, d.namespace)

	interimDidWithHint := interimDIDParts[0] + ":" + hint + interimDIDParts[1]

	logger.Infof("resolving interim did with hint: %s", interimDidWithHint)

	d.retryDID = interimDidWithHint

	return d.resolveDIDDocumentWithID(url, interimDidWithHint)
}

func (d *DIDOrbSteps) resolveDIDDocumentWithPreviousCanonicalDID(url string) error {
	logger.Infof("resolving did document with previous canonical did: %s", d.prevCanonicalDID)

	d.retryDID = d.prevCanonicalDID

	return d.resolveDIDDocumentWithID(url, d.prevCanonicalDID)
}

func (d *DIDOrbSteps) resolveDIDDocumentWithInvalidCIDInDID(url string) error {
	uniqueSuffix, err := d.getUniqueSuffix()
	if err != nil {
		return err
	}

	didWithInvalidCID := d.namespace + ":" + testCID + ":" + uniqueSuffix

	logger.Infof("resolving did document with previous invalid CID in did: %s", didWithInvalidCID)

	d.retryDID = didWithInvalidCID

	return d.resolveDIDDocumentWithID(url, didWithInvalidCID)
}

func (d *DIDOrbSteps) resolveDIDDocumentWithEquivalentDID(url string) error {
	// interim DID contains one equivalent ID
	equivalentDID := d.equivalentDID[len(d.equivalentDID)-1]

	// permanent DID has 2 or more equivalent IDs:
	// first one is canonical ID (Sidetree spec),
	// second one is with originator hint,
	// third one is with shared domain hint (if configured)
	if len(d.equivalentDID) > 1 {
		equivalentDID = d.equivalentDID[1]
	}

	logger.Infof("resolving did document with equivalent did: %s", equivalentDID)

	d.retryDID = equivalentDID

	// last equivalent ID is an ID with hints (canonical ID is always the first for published docs)
	return d.resolveDIDDocumentWithID(url, equivalentDID)
}

func (d *DIDOrbSteps) resolveDIDDocumentWithPreviousEquivalentDID(url string) error {
	prevEquivalentDID := d.prevEquivalentDID[len(d.prevEquivalentDID)-1]

	if len(d.prevEquivalentDID) > 1 {
		prevEquivalentDID = d.prevEquivalentDID[1]
	}

	logger.Infof("resolving did document with previous equivalent did: %s", prevEquivalentDID)

	d.retryDID = prevEquivalentDID

	// last equivalent ID is an ID with hints (canonical ID is always the first for published docs)
	return d.resolveDIDDocumentWithID(url, prevEquivalentDID)
}

func (d *DIDOrbSteps) resolveDIDDocumentWithInitialValue(url string) error {
	err := d.setSidetreeURL(url)
	if err != nil {
		return err
	}

	initialState, err := d.getInitialState()
	if err != nil {
		return err
	}

	interimDIDWithInitialValue := d.interimDID + initialStateSeparator + initialState

	logger.Infof("sending request with initial value: %s", d.sidetreeURL+"/"+interimDIDWithInitialValue)

	d.resp, err = d.httpClient.Get(d.sidetreeURL + "/" + interimDIDWithInitialValue)
	if err == nil && d.resp.Payload != nil {
		var result document.ResolutionResult
		err = json.Unmarshal(d.resp.Payload, &result)
		if err != nil {
			return err
		}

		err = d.prettyPrint(&result)
		if err != nil {
			return err
		}
	}

	return err
}

func (d *DIDOrbSteps) getInitialState() (string, error) {
	createReq := &model.CreateRequest{
		Delta:      d.createRequest.Delta,
		SuffixData: d.createRequest.SuffixData,
	}

	bytes, err := canonicalizer.MarshalCanonical(createReq)
	if err != nil {
		return "", err
	}

	return encoder.EncodeToString(bytes), nil
}

func getCreateRequest(strUrl string, doc []byte, patches []patch.Patch, state *state) (*ecdsa.PrivateKey, *ecdsa.PrivateKey, []byte, error) {
	recoveryKey, recoveryCommitment, err := generateKeyAndCommitment()
	if err != nil {
		return nil, nil, nil, err
	}

	updateKey, updateCommitment, err := generateKeyAndCommitment()
	if err != nil {
		return nil, nil, nil, err
	}

	origin, err := state.getAnchorOrigin(strUrl)
	if err != nil {
		return nil, nil, nil, err
	}

	reqBytes, err := client.NewCreateRequest(&client.CreateRequestInfo{
		OpaqueDocument:     string(doc),
		Patches:            patches,
		RecoveryCommitment: recoveryCommitment,
		UpdateCommitment:   updateCommitment,
		MultihashCode:      sha2_256,
		AnchorOrigin:       origin,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	return recoveryKey, updateKey, reqBytes, nil
}

func (d *DIDOrbSteps) getRecoverRequest(doc []byte, patches []patch.Patch, uniqueSuffix string) ([]byte, *ecdsa.PrivateKey, *ecdsa.PrivateKey, error) {
	currentRecoveryKey := d.getLatestRecoveryKey()

	nextRecoveryKey, nextRecoveryCommitment, err := generateKeyAndCommitment()
	if err != nil {
		return nil, nil, nil, err
	}

	nextUpdateKey, nextUpdateCommitment, err := generateKeyAndCommitment()
	if err != nil {
		return nil, nil, nil, err
	}

	// recovery key and signer passed in are generated during previous operations
	recoveryPubKey, err := pubkey.GetPublicKeyJWK(&currentRecoveryKey.PublicKey)
	if err != nil {
		return nil, nil, nil, err
	}

	revealValue, err := commitment.GetRevealValue(recoveryPubKey, sha2_256)
	if err != nil {
		return nil, nil, nil, err
	}

	now := time.Now().Unix()

	origin, err := d.state.getAnchorOrigin(d.sidetreeURL)
	if err != nil {
		return nil, nil, nil, err
	}

	recoverRequest, err := client.NewRecoverRequest(&client.RecoverRequestInfo{
		DidSuffix:          uniqueSuffix,
		RevealValue:        revealValue,
		OpaqueDocument:     string(doc),
		Patches:            patches,
		RecoveryKey:        recoveryPubKey,
		RecoveryCommitment: nextRecoveryCommitment,
		UpdateCommitment:   nextUpdateCommitment,
		MultihashCode:      sha2_256,
		Signer:             ecsigner.New(currentRecoveryKey, "ES256", ""), // sign with old signer
		AnchorFrom:         now,
		AnchorUntil:        now + anchorTimeDelta,
		AnchorOrigin:       origin,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	return recoverRequest, nextRecoveryKey, nextUpdateKey, nil
}

func (d *DIDOrbSteps) getUniqueSuffix() (string, error) {
	return getUniqueSuffix(d.createRequest.SuffixData)
}

func getUniqueSuffix(suffixData *model.SuffixDataModel) (string, error) {
	return hashing.CalculateModelMultihash(suffixData, sha2_256)
}

func (d *DIDOrbSteps) getDeactivateRequest(did string) ([]byte, error) {
	currentRecoveryKey := d.getLatestRecoveryKey()

	// recovery key and signer passed in are generated during previous operations
	recoveryPubKey, err := pubkey.GetPublicKeyJWK(&currentRecoveryKey.PublicKey)
	if err != nil {
		return nil, err
	}

	revealValue, err := commitment.GetRevealValue(recoveryPubKey, sha2_256)
	if err != nil {
		return nil, err
	}

	return client.NewDeactivateRequest(&client.DeactivateRequestInfo{
		DidSuffix:   did,
		RevealValue: revealValue,
		RecoveryKey: recoveryPubKey,
		Signer:      ecsigner.New(currentRecoveryKey, "ES256", ""),
		AnchorFrom:  time.Now().Unix(),
	})
}

func (d *DIDOrbSteps) getLatestRecoveryKey() *ecdsa.PrivateKey {
	return d.recoveryKeys[len(d.recoveryKeys)-1]
}

func (d *DIDOrbSteps) getLatestUpdateKey() *ecdsa.PrivateKey {
	return d.updateKeys[len(d.updateKeys)-1]
}

func (d *DIDOrbSteps) getUpdateRequest(did string, patches []patch.Patch) ([]byte, *ecdsa.PrivateKey, error) {
	return getUpdateRequest(did, d.getLatestUpdateKey(), patches)
}

func getUpdateRequest(did string, currentUpdateKey *ecdsa.PrivateKey, patches []patch.Patch) ([]byte, *ecdsa.PrivateKey, error) {
	nextUpdateKey, nextUpdateCommitment, err := generateKeyAndCommitment()
	if err != nil {
		return nil, nil, err
	}

	// update key and signer passed in are generated during previous operations
	updatePubKey, err := pubkey.GetPublicKeyJWK(&currentUpdateKey.PublicKey)
	if err != nil {
		return nil, nil, err
	}

	revealValue, err := commitment.GetRevealValue(updatePubKey, sha2_256)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().Unix()

	req, err := client.NewUpdateRequest(&client.UpdateRequestInfo{
		DidSuffix:        did,
		RevealValue:      revealValue,
		UpdateCommitment: nextUpdateCommitment,
		UpdateKey:        updatePubKey,
		Patches:          patches,
		MultihashCode:    sha2_256,
		Signer:           ecsigner.New(currentUpdateKey, "ES256", ""),
		AnchorFrom:       now,
		AnchorUntil:      now + anchorTimeDelta,
	})
	if err != nil {
		return nil, nil, err
	}

	return req, nextUpdateKey, nil
}

func generateKeyAndCommitment() (*ecdsa.PrivateKey, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}

	pubKey, err := pubkey.GetPublicKeyJWK(&key.PublicKey)
	if err != nil {
		return nil, "", err
	}

	c, err := commitment.GetCommitment(pubKey, sha2_256)
	if err != nil {
		return nil, "", err
	}

	return key, c, nil
}

func getJSONPatch(path, value string) (patch.Patch, error) {
	patches := fmt.Sprintf(`[{"op": "replace", "path":  "%s", "value": "%s"}]`, path, value)
	logger.Infof("creating JSON patch: %s", patches)
	return patch.NewJSONPatch(patches)
}

func getAddPublicKeysPatch(keyID string) (patch.Patch, error) {
	addPubKeys := fmt.Sprintf(addPublicKeysTemplate, keyID)
	logger.Infof("creating add public keys patch: %s", addPubKeys)
	return patch.NewAddPublicKeysPatch(addPubKeys)
}

func getRemovePublicKeysPatch(keyID string) (patch.Patch, error) {
	removePubKeys := fmt.Sprintf(removePublicKeysTemplate, keyID)
	logger.Infof("creating remove public keys patch: %s", removePubKeys)
	return patch.NewRemovePublicKeysPatch(removePubKeys)
}

func getAddServiceEndpointsPatch(svcID string) (patch.Patch, error) {
	addServices := fmt.Sprintf(addServicesTemplate, svcID)
	logger.Infof("creating add service endpoints patch: %s", addServices)
	return patch.NewAddServiceEndpointsPatch(addServices)
}

func getRemoveServiceEndpointsPatch(keyID string) (patch.Patch, error) {
	removeServices := fmt.Sprintf(removeServicesTemplate, keyID)
	logger.Infof("creating remove service endpoints patch: %s", removeServices)
	return patch.NewRemoveServiceEndpointsPatch(removeServices)
}

func getAddAlsoKnownAsPatch(uri string) (patch.Patch, error) {
	addURIs := fmt.Sprintf(`["%s"]`, uri)
	logger.Infof("creating add also known as patch: %s", uri)
	return patch.NewAddAlsoKnownAs(addURIs)
}

func getRemoveAlsoKnownAsPatch(uri string) (patch.Patch, error) {
	removeURIs := fmt.Sprintf(`["%s"]`, uri)
	logger.Infof("creating remove also known as patch: %s", removeURIs)
	return patch.NewRemoveAlsoKnownAs(removeURIs)
}

func getOpaqueDocument(keyID string) ([]byte, error) {
	// create general + auth JWS verification key
	jwsPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	jwsPubKey, err := getPubKey(&jwsPrivateKey.PublicKey)
	if err != nil {
		return nil, err
	}

	// create general + assertion ed25519 verification key
	ed25519PulicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	ed25519PubKey, err := getPubKey(ed25519PulicKey)
	if err != nil {
		return nil, err
	}

	recipientKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	data := fmt.Sprintf(
		docTemplate,
		keyID, jwsPubKey, ed25519PubKey, base58.Encode(recipientKey))

	doc, err := document.FromBytes([]byte(data))
	if err != nil {
		return nil, err
	}

	return doc.Bytes()
}

func getPubKey(pubKey interface{}) (string, error) {
	publicKey, err := pubkey.GetPublicKeyJWK(pubKey)
	if err != nil {
		return "", err
	}

	opsPubKeyBytes, err := json.Marshal(publicKey)
	if err != nil {
		return "", err
	}

	return string(opsPubKeyBytes), nil
}

func (d *DIDOrbSteps) prettyPrint(result *document.ResolutionResult) error {
	if d.didPrintEnabled {

		b, err := json.MarshalIndent(result, "", " ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
	}

	return nil
}

func (d *DIDOrbSteps) createDIDDocuments(strURLs string, num int, concurrency int) error {
	logger.Infof("creating %d DID document(s) at %s using a concurrency of %d", num, strURLs, concurrency)

	return d.createDIDDocumentsAtURLs(strings.Split(strURLs, ","), num, concurrency, 30)
}

func (d *DIDOrbSteps) createDIDDocumentsAsync(strURLs string, num int, concurrency int) error {
	logger.Infof("started Go routine to create %d DID document(s) at %s using a concurrency of %d",
		num, strURLs, concurrency)

	go func() {
		if err := d.createDIDDocuments(strURLs, num, concurrency); err != nil {
			logger.Errorf("Error creating DID documents: %s", err)
		}
	}()

	return nil
}

func (d *DIDOrbSteps) createAndUpdateDIDDocuments(strURLs string, num int, updateKeyID string, concurrency int) error {
	logger.Infof("creating and updating %d DID document(s) at %s using a concurrency of %d", num, strURLs, concurrency)

	return d.createAndUpdateDIDDocumentsAtURLs(strings.Split(strURLs, ","), updateKeyID, num, concurrency, 30)
}

func (d *DIDOrbSteps) createAndUpdateDIDDocumentsAsync(strURLs string, num int, updateKeyID string, concurrency int) error {
	logger.Infof("started Go routine to create and update %d DID document(s) at %s using a concurrency of %d",
		num, strURLs, concurrency)

	go func() {
		if err := d.createAndUpdateDIDDocuments(strURLs, num, updateKeyID, concurrency); err != nil {
			logger.Errorf("Error creating DID documents: %s", err)
		}
	}()

	return nil
}

func (d *DIDOrbSteps) createDIDDocumentsAtURLs(urls []string, num, concurrency, attempts int) error {
	logger.Infof("creating %d DID document(s) at %s using a concurrency of %d", num, urls, concurrency)

	return performDIDOperations[*createDIDResponse](
		fmt.Sprintf("Create %d DID documents", num),
		num, concurrency, d.createResponses,
		func() Request[*createDIDResponse] {
			return newCreateDIDRequest(d.state, d.httpClient, urls, attempts, 10*time.Second,
				func(resp *httpResponse, err error) bool {
					if err != nil {
						return strings.Contains(strings.ToLower(err.Error()), strings.ToLower("EOF")) ||
							strings.Contains(strings.ToLower(err.Error()), strings.ToLower("connection refused"))
					}

					return resp.StatusCode >= 500
				},
			)
		},
	)
}

func (d *DIDOrbSteps) createAndUpdateDIDDocumentsAtURLs(urls []string, updateKey string, num, concurrency, attempts int) error {
	logger.Infof("creating and updating %d DID document(s) at %s using a concurrency of %d", num, urls, concurrency)

	return performDIDOperations(
		fmt.Sprintf("Create and update %d DID documents", num),
		num, concurrency, d.createAndUpdateResponses.responses,
		func() Request[*createAndUpdateDIDResponse] {
			return newCreateAndUpdateDIDRequest(d.state, d.httpClient, urls, updateKey, attempts, 10*time.Second,
				func(resp *httpResponse, err error) bool {
					if err != nil {
						return strings.Contains(strings.ToLower(err.Error()), strings.ToLower("EOF")) ||
							strings.Contains(strings.ToLower(err.Error()), strings.ToLower("connection refused"))
					}

					return resp.StatusCode >= 500
				},
			)
		},
	)
}

func performDIDOperations[T didResponse](taskDesc string, num, concurrency int,
	responses *responses[T], newRequest func() Request[T],
) error {
	responses.Clear()

	p := NewWorkerPool[T](concurrency, WithTaskDescription(taskDesc))

	p.Start()

	for i := 0; i < num; i++ {
		p.Submit(newRequest())
	}

	p.Stop()

	logger.Infof("got %d responses for %d requests", len(p.responses), num)

	if len(p.responses) != num {
		return fmt.Errorf("expecting %d responses but got %d", num, len(p.responses))
	}

	for _, resp := range p.responses {
		req := resp.Request
		if resp.Err != nil {
			logger.Infof("Create DID documents got error from [%s]: %s", req.URL(), resp.Err)

			return resp.Err
		}

		logger.Infof("got DID from [%s]: %s", req.URL(), resp.Resp.DID())

		responses.Add(resp.Resp)
	}

	return nil
}

func (d *DIDOrbSteps) createDIDDocumentsAndStoreDIDsToFile(strURLs, strNum, strConcurrency string, file string) error {
	return d.createDIDDocumentsWithRetriesAndStoreDIDsToFile(strURLs, strNum, strConcurrency, "30", file)
}

func (d *DIDOrbSteps) createDIDDocumentsWithRetriesAndStoreDIDsToFile(strURLs, strNum, strConcurrency string, strMaxAttempts string, file string) error {
	if err := d.state.resolveVarsInExpression(&strURLs, &file, &strNum, &strConcurrency, &strMaxAttempts); err != nil {
		return err
	}

	num, err := strconv.Atoi(strNum)
	if err != nil {
		return fmt.Errorf("invalid value for number of DIDs: %w", err)
	}

	concurrency, err := strconv.Atoi(strConcurrency)
	if err != nil {
		return fmt.Errorf("invalid value for concurrency: %w", err)
	}

	if strMaxAttempts == "" {
		strMaxAttempts = "1"
	}

	maxAttempts, err := strconv.Atoi(strMaxAttempts)
	if err != nil {
		return fmt.Errorf("invalid value for maximum attempts: %w", err)
	}

	urls := strings.Split(strURLs, ",")

	localUrls := make([]string, len(urls))

	for i, u := range urls {
		localUrls[i] = fmt.Sprintf("%s/sidetree/v1/operations", u)
	}

	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	logger.Warnf("creating %d DID document(s) at %s using a concurrency of %d and a maximum of %d attempt(s) and storing to file [%s]",
		num, urls, concurrency, maxAttempts, file)

	err = d.createDIDDocumentsAtURLs(localUrls, num, concurrency, maxAttempts)
	if err != nil {
		return err
	}

	f, err := os.Create(file)
	if err != nil {
		return err
	}

	defer func() {
		if e := f.Close(); e != nil {
			logger.Warnf("Error closing file [%s]: %s", file, err)
		}
	}()

	responses := d.createResponses.GetAll()

	for _, resp := range responses {
		_, e := f.WriteString(fmt.Sprintf("%s\n", resp.did))
		if e != nil {
			return e
		}
	}

	err = f.Sync()
	if err != nil {
		return err
	}

	logger.Warnf("Wrote %d DIDs to file [%s]", len(responses), file)

	return nil
}

func (d *DIDOrbSteps) waitForCreateDIDDocuments(waitDuration string, num int) error {
	wd, err := time.ParseDuration(waitDuration)
	if err != nil {
		return fmt.Errorf("invalid wait duration [%s]: %w", waitDuration, err)
	}

	logger.Infof("Waiting up to %s for %d DID documents to be created ...", wd, num)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(wd))
	defer cancel()

	startTime := time.Now()

	_, err = d.createResponses.Get(ctx, num)
	if err != nil {
		return fmt.Errorf("failed to get %d DIDs after %s: %w", num, wd, err)
	}

	logger.Infof("... %d DID documents were successfully created after %s", num, time.Since(startTime))

	return nil
}

func (d *DIDOrbSteps) waitForCreateAndUpdateDIDDocuments(waitDuration string, num int) error {
	wd, err := time.ParseDuration(waitDuration)
	if err != nil {
		return fmt.Errorf("invalid wait duration [%s]: %w", waitDuration, err)
	}

	logger.Infof("Waiting up to %s for %d DID documents to be created and updated ...", wd, num)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(wd))
	defer cancel()

	startTime := time.Now()

	_, err = d.createAndUpdateResponses.Get(ctx, num)
	if err != nil {
		return fmt.Errorf("failed to get %d DIDs after %s: %w", num, wd, err)
	}

	logger.Infof("... %d DID documents were successfully created and updated after %s", num, time.Since(startTime))

	return nil
}

func (d *DIDOrbSteps) updateDIDDocuments(strURLs string, keyID string, concurrency int) error {
	responses := d.createResponses.GetAll()

	num := len(responses)

	logger.Infof("updating %d DID document(s) at %s using a concurrency of %d", num, strURLs, concurrency)

	urls := strings.Split(strURLs, ",")

	ptch, err := getAddPublicKeysPatch(keyID)
	if err != nil {
		return err
	}

	d.updateResponses = nil

	p := NewWorkerPool[*updateDIDResponse](concurrency,
		WithTaskDescription(fmt.Sprintf("Update %d DID documents", num)))

	p.Start()

	for i := 0; i < num; i++ {
		createResp := responses[i]

		createReq := &model.CreateRequest{}
		if err := json.Unmarshal(createResp.reqBytes, createReq); err != nil {
			return err
		}

		suffix, err := getUniqueSuffix(createReq.SuffixData)
		if err != nil {
			return err
		}

		p.Submit(&updateDIDRequest{
			url:        urls[mrand.Intn(len(urls))],
			did:        createResp.did,
			suffix:     suffix,
			httpClient: d.httpClient,
			suffixData: createReq.SuffixData,
			updateKey:  createResp.updateKey,
			patches:    []patch.Patch{ptch},
		})
	}

	p.Stop()

	logger.Infof("got %d responses for %d requests", len(p.responses), num)

	if len(p.responses) != num {
		return fmt.Errorf("expecting %d responses but got %d", num, len(p.responses))
	}

	for _, resp := range p.responses {
		req := resp.Request
		if resp.Err != nil {
			logger.Infof("Update DID documents got error from [%s]: %s", req.URL(), resp.Err)
			return resp.Err
		}

		d.updateResponses = append(d.updateResponses, resp.Resp)
	}

	return nil
}

func (d *DIDOrbSteps) updateDIDDocumentsAgain(strURLs string, keyID string, concurrency int) error {
	updateResponses := d.updateResponses

	num := len(updateResponses)

	logger.Infof("updating %d DID document(s) again at %s using a concurrency of %d",
		num, strURLs, concurrency)

	urls := strings.Split(strURLs, ",")

	ptch, err := getAddPublicKeysPatch(keyID)
	if err != nil {
		return err
	}

	d.updateResponses = nil

	p := NewWorkerPool[*updateDIDResponse](concurrency,
		WithTaskDescription(fmt.Sprintf("Update %d DID documents", num)))

	p.Start()

	for i := 0; i < num; i++ {
		updateResp := updateResponses[i]

		suffix, err := getUniqueSuffix(updateResp.suffixData)
		if err != nil {
			return err
		}

		p.Submit(&updateDIDRequest{
			url:        urls[mrand.Intn(len(urls))],
			did:        updateResp.did,
			suffix:     suffix,
			httpClient: d.httpClient,
			suffixData: updateResp.suffixData,
			updateKey:  updateResp.updateKey,
			patches:    []patch.Patch{ptch},
		})
	}

	p.Stop()

	logger.Infof("got %d responses for %d requests", len(p.responses), num)

	if len(p.responses) != num {
		return fmt.Errorf("expecting %d responses but got %d", num, len(p.responses))
	}

	for _, resp := range p.responses {
		req := resp.Request
		if resp.Err != nil {
			logger.Infof("Update DID documents again got error from [%s]: %s", req.URL(), resp.Err)
			return resp.Err
		}

		d.updateResponses = append(d.updateResponses, resp.Resp)
	}

	return nil
}

func (d *DIDOrbSteps) verifyDIDDocuments(strURLs string) error {
	responses := d.createResponses.GetAll()

	logger.Infof("Verifying the %d DID document(s) that were created", len(responses))

	urls := strings.Split(strURLs, ",")

	for i, resp := range responses {
		canonicalID, err := d.verifyDID(urls[mrand.Intn(len(urls))], resp.did, 50)
		if err != nil {
			return err
		}

		responses[i].did = canonicalID

		logger.Infof("... verified %d out of %d DIDs", i+1, len(responses))
	}

	return nil
}

func (d *DIDOrbSteps) verifyDIDDocumentsFromFile(strURLs, file, strAttempts string) error {
	if err := d.state.resolveVarsInExpression(&strURLs, &file, &strAttempts); err != nil {
		return err
	}

	attempts, err := strconv.Atoi(strAttempts)
	if err != nil {
		return fmt.Errorf("invalid value for attempts: %w", err)
	}

	reader, err := d.newReader(file)
	if err != nil {
		return fmt.Errorf("get DID file from [%s]: %w", file, err)
	}

	logger.Infof("Verifying created DIDs from file [%s] at %s with %d retry attempt(s)",
		file, strURLs, attempts)

	scanner := bufio.NewScanner(reader)

	var dids []string

	for scanner.Scan() {
		dids = append(dids, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		logger.Errorf("Error verifying created DIDs from file [%s]: %s", file, err)

		return err
	}

	urls := strings.Split(strURLs, ",")

	for i, did := range dids {
		_, e := d.verifyDID(fmt.Sprintf("%s/sidetree/v1/identifiers", urls[mrand.Intn(len(urls))]), did, attempts)
		if e != nil {
			return e
		}

		if (i+1)%100 == 0 {
			logger.Warnf("... verified %d out of %d DIDs", i+1, len(dids))
		}
	}

	logger.Warnf("... verified %d DIDs from file [%s]", len(dids), file)

	return nil
}

func (d *DIDOrbSteps) verifyUpdatedDIDDocuments(strURLs string, keyID string) error {
	return d.doVerifyUpdatedDIDDocuments(d.updateResponses, strURLs, keyID)
}

func (d *DIDOrbSteps) verifyCreatedAndUpdatedDIDDocuments(strURLs string, keyID string) error {
	return d.doVerifyUpdatedDIDDocuments(d.createAndUpdateResponses.UpdateResponses(), strURLs, keyID)
}

func (d *DIDOrbSteps) doVerifyUpdatedDIDDocuments(updateResponses []*updateDIDResponse, strURLs string, keyID string) error {
	logger.Infof("Verifying the %d DID document(s) that were updated with key ID [%s]",
		len(updateResponses), keyID)

	urls := strings.Split(strURLs, ",")

	const maxAttempts = 30

	for i, resp := range updateResponses {
		verified := false

		var err error

		for attempt := 0; attempt < maxAttempts; attempt++ {
			err = d.doVerifyUpdatedDIDContainsKeyID(urls, resp.did, keyID)
			if err != nil {
				logger.Infof("... updated DID %s not verified on attempt %d: %s", resp.did, attempt+1, err)

				time.Sleep(5 * time.Second)

				continue
			}

			logger.Infof("... verified %d out of %d updated DIDs", i+1, len(updateResponses))

			verified = true

			break
		}

		if !verified {
			return fmt.Errorf("updated DID %s not verified after %d attempts: %s",
				resp.did, maxAttempts, err)
		}
	}

	logger.Infof("Successfully verified %d updated DIDs", len(updateResponses))

	return nil
}

func (d *DIDOrbSteps) doVerifyUpdatedDIDContainsKeyID(urls []string, did, keyID string) error {
	randomURL := urls[mrand.Intn(len(urls))]

	if err := d.verifyUpdatedDIDContainsKeyID(randomURL, did, keyID); err != nil {
		return err
	}

	return nil
}

func (d *DIDOrbSteps) verifyDID(url, did string, attempts int) (canonicalID string, err error) {
	logger.Infof("verifying DID %s from %s", did, url)

	resp, err := d.httpClient.GetWithRetryFunc(url+"/"+did, attempts,
		func(resp *httpResponse) bool {
			if resp.StatusCode == http.StatusNotFound {
				return true
			}

			if resp.StatusCode != http.StatusOK {
				return resp.StatusCode >= http.StatusInternalServerError
			}

			var rr document.ResolutionResult
			err = json.Unmarshal(resp.Payload, &rr)
			if err != nil {
				return false
			}

			_, ok := rr.DocumentMetadata["canonicalId"]
			if !ok {
				// The DID is not anchored yet. Retry until it's anchored.
				logger.Infof("Document metadata is missing field 'canonicalId'. Retrying.")

				return true
			}

			return false
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to resolve DID[%s]: %w", did, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to resolve DID [%s] - Status code %d: %s", did, resp.StatusCode, resp.ErrorMsg)
	}

	var rr document.ResolutionResult
	err = json.Unmarshal(resp.Payload, &rr)
	if err != nil {
		return "", err
	}

	cID, ok := rr.DocumentMetadata["canonicalId"]
	if !ok {
		return "", fmt.Errorf("document metadata is missing field 'canonicalId': %s", resp.Payload)
	}

	canonicalID = cID.(string)

	logger.Infof(".. successfully verified DID %s from %s", canonicalID, url)

	return canonicalID, nil
}

func (d *DIDOrbSteps) verifyUpdatedDIDContainsKeyID(url, did, keyID string) error {
	logger.Infof("verifying updated DID %s contains key ID [%s] from %s", did, keyID, url)

	resp, err := d.httpClient.Get(url + "/" + did)
	if err != nil {
		return fmt.Errorf("failed to resolve DID[%s]: %w", did, err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to resolve DID [%s] - Status code %d: %s", did, resp.StatusCode, resp.ErrorMsg)
	}

	logger.Infof("Got updated DID document: %s", resp.Payload)

	var rr document.ResolutionResult
	err = json.Unmarshal(resp.Payload, &rr)
	if err != nil {
		return err
	}

	authentication, ok := rr.Document["authentication"]
	if !ok {
		return fmt.Errorf("document is missing field 'authentication': %s", resp.Payload)
	}

	authArr, ok := authentication.([]interface{})
	if !ok {
		return fmt.Errorf("expecting 'authentication' field to be an array but got %s",
			reflect.TypeOf(authentication))
	}

	for _, v := range authArr {
		auth, ok := v.(string)
		if !ok {
			return fmt.Errorf("expecting 'authentication' value to be a string but got %s",
				reflect.TypeOf(auth))
		}

		if strings.Contains(auth, keyID) {
			logger.Infof(".. successfully verified that updated DID %s contains key ID [%s]",
				did, keyID)

			return nil
		}
	}

	return fmt.Errorf("DID %s does not contain key ID [%s]", did, keyID)
}

func (d *DIDOrbSteps) newReader(file string) (io.Reader, error) {
	if u, err := url.Parse(file); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		httpClient := newHTTPClient(d.state, d.bddContext)
		resp, e := httpClient.Get(file)
		if e != nil {
			return nil, fmt.Errorf("new reader from [%s]: %w", file, e)
		}

		logger.Infof("Got header: %s", resp.Header)

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("new reader from [%s] returned status %d: %s", file, resp.StatusCode, resp.ErrorMsg)
		}

		if strings.Contains(resp.Header.Get("Content-Type"), "application/zip") {
			reader, e := readZip(resp.Payload)
			if e != nil {
				return nil, fmt.Errorf("new reader from [%s]: %w", file, e)
			}

			return reader, nil
		}

		return bytes.NewReader(resp.Payload), nil
	}

	f, e := os.Open(file)
	if e != nil {
		return nil, fmt.Errorf("open file [%s]: %w", file, e)
	}

	defer func() {
		if e := f.Close(); e != nil {
			logger.Warnf("Error closing file [%s]: %s", file, e)
		}
	}()

	contents, e := io.ReadAll(f)
	if e != nil {
		return nil, fmt.Errorf("read file [%s]: %w", file, e)
	}

	if strings.HasSuffix(file, ".zip") {
		reader, e := readZip(contents)
		if e != nil {
			return nil, fmt.Errorf("read zip file [%s]: %w", file, e)
		}

		return reader, nil
	}

	return bytes.NewReader(contents), nil
}

func (d *DIDOrbSteps) setAnchorOrigin(host, anchorOrigin string) error {
	if err := d.state.resolveVarsInExpression(&host, &anchorOrigin); err != nil {
		return err
	}

	d.state.setAnchorOrigin(host, anchorOrigin)

	return nil
}

type createDIDRequest struct {
	urls        []string
	url         string
	httpClient  *httpClient
	shouldRetry func(*httpResponse, error) bool
	maxAttempts int
	backoff     time.Duration
	greylist    *greylist
	state       *state
}

type createDIDResponse struct {
	did         string
	recoveryKey *ecdsa.PrivateKey
	updateKey   *ecdsa.PrivateKey
	reqBytes    []byte
}

func (r *createDIDResponse) DID() string {
	return r.did
}

func newCreateDIDRequest(state *state, httpClient *httpClient, urls []string, attempts int, greylistDuration time.Duration,
	shouldRetry func(*httpResponse, error) bool,
) *createDIDRequest {
	return &createDIDRequest{
		urls:        urls,
		httpClient:  httpClient,
		shouldRetry: shouldRetry,
		maxAttempts: attempts,
		backoff:     3 * time.Second,
		greylist:    newGreylist(greylistDuration),
		state:       state,
	}
}

func (r *createDIDRequest) URL() string {
	return r.url
}

func (r *createDIDRequest) Invoke() (*createDIDResponse, error) {
	var (
		resp                   *httpResponse
		respErr                error
		recoveryKey, updateKey *ecdsa.PrivateKey
		reqBytes               []byte
	)

	maxAttempts := r.maxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	index := mrand.Intn(len(r.urls))

	for i := 0; i < maxAttempts; i++ {
		if index >= len(r.urls) {
			index = 0
		}

		u := r.urls[index]

		if len(r.urls) > 0 && r.greylist.IsGreylisted(u) {
			continue
		}

		r.url = u

		// A retry will be performed on a different URL.
		index++

		logger.Infof("creating DID document at %s", u)

		opaqueDoc, err := getOpaqueDocument("key1")
		if err != nil {
			return nil, err
		}

		recoveryKey, updateKey, reqBytes, err = getCreateRequest(u, opaqueDoc, nil, r.state)
		if err != nil {
			return nil, err
		}

		logger.Infof("Posting request to [%s] on attempt #%d", u, i+1)

		resp, respErr = r.httpClient.Post(u, reqBytes, "application/json")
		if respErr != nil {
			if r.shouldRetry(nil, respErr) {
				logger.Warnf("Error posting request to [%s] on attempt %d: %s. Retrying in %s",
					u, i+1, respErr, r.backoff)

				r.greylist.Add(u)

				time.Sleep(r.backoff)

				continue
			}

			logger.Errorf("Error posting request to [%s]: %s. Not retrying.", u, respErr)

			return nil, respErr
		}

		if resp.StatusCode == http.StatusOK {
			respErr = nil

			break
		}

		if !r.shouldRetry(resp, nil) {
			logger.Errorf("Got HTTP response from [%s]: %d:%s. Not retrying.", u, resp.StatusCode, resp.ErrorMsg)

			return nil, fmt.Errorf("status code: %d: %s", resp.StatusCode, resp.ErrorMsg)
		}

		logger.Warnf("Got HTTP response from [%s]: %d:%s. Retrying in %s", u, resp.StatusCode, resp.ErrorMsg, r.backoff)

		time.Sleep(r.backoff)
	}

	if respErr != nil {
		return nil, respErr
	}

	logger.Infof("... got DID document: %s", resp.Payload)

	var rr document.ResolutionResult
	err := json.Unmarshal(resp.Payload, &rr)
	if err != nil {
		return nil, err
	}

	return &createDIDResponse{
		did:         rr.Document.ID(),
		recoveryKey: recoveryKey,
		updateKey:   updateKey,
		reqBytes:    reqBytes,
	}, nil
}

type updateDIDRequest struct {
	url        string
	did        string
	suffix     string
	httpClient *httpClient
	suffixData *model.SuffixDataModel
	updateKey  *ecdsa.PrivateKey
	patches    []patch.Patch
}

type updateDIDResponse struct {
	did        string
	updateKey  *ecdsa.PrivateKey
	suffixData *model.SuffixDataModel
}

func (r *updateDIDRequest) URL() string {
	return r.url
}

func (r *updateDIDRequest) Invoke() (*updateDIDResponse, error) {
	uniqueSuffix, err := getUniqueSuffix(r.suffixData)
	if err != nil {
		return nil, err
	}

	logger.Infof("updating DID [%s] document at %s", uniqueSuffix, r.url)

	reqBytes, newxtUpdate, err := getUpdateRequest(r.suffix, r.updateKey, r.patches)
	if err != nil {
		return nil, err
	}

	resp, err := r.httpClient.Post(r.url, reqBytes, "application/json")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("did [%s] - http status code %d - %s", uniqueSuffix, resp.StatusCode, resp.ErrorMsg)
	}

	logger.Infof("... successfully updated DID document [%s]", uniqueSuffix)

	return &updateDIDResponse{
		did:        r.did,
		updateKey:  newxtUpdate,
		suffixData: r.suffixData,
	}, nil
}

type createAndUpdateDIDRequest struct {
	*createDIDRequest

	keyID string
}

type createAndUpdateDIDResponse struct {
	*createDIDResponse
	*updateDIDResponse
}

func newCreateAndUpdateDIDRequest(state *state, httpClient *httpClient, urls []string, updateKeyID string, attempts int, greylistDuration time.Duration,
	shouldRetry func(*httpResponse, error) bool,
) *createAndUpdateDIDRequest {
	return &createAndUpdateDIDRequest{
		createDIDRequest: newCreateDIDRequest(state, httpClient, urls, attempts, greylistDuration, shouldRetry),
		keyID:            updateKeyID,
	}
}

func (r *createAndUpdateDIDRequest) URL() string {
	return r.createDIDRequest.URL()
}

func (r *createAndUpdateDIDRequest) Invoke() (*createAndUpdateDIDResponse, error) {
	createResp, err := r.createDIDRequest.Invoke()
	if err != nil {
		return nil, fmt.Errorf("create DID: %w", err)
	}

	createReq := &model.CreateRequest{}
	if err := json.Unmarshal(createResp.reqBytes, createReq); err != nil {
		return nil, err
	}

	suffix, err := getUniqueSuffix(createReq.SuffixData)
	if err != nil {
		return nil, err
	}

	ptch, err := getAddPublicKeysPatch(r.keyID)
	if err != nil {
		return nil, err
	}

	updateRequest := &updateDIDRequest{
		url:        r.url,
		did:        createResp.did,
		suffix:     suffix,
		httpClient: r.httpClient,
		suffixData: createReq.SuffixData,
		updateKey:  createResp.updateKey,
		patches:    []patch.Patch{ptch},
	}

	updateResp, err := updateRequest.Invoke()
	if err != nil {
		return nil, fmt.Errorf("update DID: %w", err)
	}

	return &createAndUpdateDIDResponse{
		createDIDResponse: createResp,
		updateDIDResponse: updateResp,
	}, nil
}

// RegisterSteps registers orb steps
func (d *DIDOrbSteps) RegisterSteps(s *godog.Suite) {
	s.Step(`^client discover orb endpoints$`, d.discoverEndpoints)
	s.Step(`^client sends request to "([^"]*)" to request anchor origin$`, d.clientRequestsAnchorOrigin)
	s.Step(`^client verifies resolved document$`, d.clientVerifiesResolvedDocument)
	s.Step(`^client verifies that web document from variable "([^"]*)" is produced from orb document from variable "([^"]*)"$`, d.clientVerifiesWebDocumentFromOrbDocument)
	s.Step(`^mis-configured client fails to verify resolved document$`, d.clientFailsToVerifyResolvedDocument)
	s.Step(`^check error response contains "([^"]*)"$`, d.checkErrorResp)
	s.Step(`^client sends request to "([^"]*)" to create DID document$`, d.createDIDDocument)
	s.Step(`^client sends request to "([^"]*)" to create DID document and the ID is saved to variable "([^"]*)"$`, d.createDIDDocumentSaveIDToVar)
	s.Step(`^client sends request to "([^"]*)" to create DID document and the suffix is saved to variable "([^"]*)"$`, d.createDIDDocumentSaveSuffixToVar)
	s.Step(`^check success response contains "([^"]*)"$`, d.checkSuccessRespContains)
	s.Step(`^check success response does NOT contain "([^"]*)"$`, d.checkSuccessRespDoesntContain)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with interim did$`, d.resolveDIDDocumentWithInterimDID)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with canonical did$`, d.resolveDIDDocumentWithCanonicalDID)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with canonical did and version ID "([^"]*)"$`, d.resolveDIDDocumentWithCanonicalDIDAndVersionID)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with canonical did and version time "([^"]*)"$`, d.resolveDIDDocumentWithCanonicalDIDAndVersionTime)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with canonical did and resets keys to last successful$`, d.resetKeysToLastSuccessful)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with previous canonical did$`, d.resolveDIDDocumentWithPreviousCanonicalDID)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with invalid CID in canonical did$`, d.resolveDIDDocumentWithInvalidCIDInDID)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with equivalent did$`, d.resolveDIDDocumentWithEquivalentDID)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with previous equivalent did$`, d.resolveDIDDocumentWithPreviousEquivalentDID)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with hint "([^"]*)"`, d.resolveDIDDocumentWithHint)
	s.Step(`^client sends request to "([^"]*)" to resolve interim DID document with hint "([^"]*)"`, d.resolveInterimDIDDocumentWithHint)
	s.Step(`^^client sends request to "([^"]*)" to add (\d+) public keys to DID document$`, d.addNPublicKeysToDIDDocument)
	s.Step(`^client sends request to "([^"]*)" to add public key with ID "([^"]*)" to DID document$`, d.addPublicKeyToDIDDocument)
	s.Step(`^client sends request to "([^"]*)" to remove public key with ID "([^"]*)" from DID document$`, d.removePublicKeyFromDIDDocument)
	s.Step(`^client sends request to "([^"]*)" to add service endpoint with ID "([^"]*)" to DID document$`, d.addServiceEndpointToDIDDocument)
	s.Step(`^client sends request to "([^"]*)" to remove service endpoint with ID "([^"]*)" from DID document$`, d.removeServiceEndpointsFromDIDDocument)
	s.Step(`^client sends request to "([^"]*)" to add also known as URI "([^"]*)" to DID document$`, d.addAlsoKnownAsURIToDIDDocument)
	s.Step(`^client sends request to "([^"]*)" to remove also known as URI "([^"]*)" from DID document$`, d.removeAlsoKnownAsURIFromDIDDocument)
	s.Step(`^client sends request to "([^"]*)" to deactivate DID document$`, d.deactivateDIDDocument)
	s.Step(`^client sends request to "([^"]*)" to recover DID document$`, d.recoverDIDDocument)
	s.Step(`^client sends request to "([^"]*)" to resolve DID document with initial state$`, d.resolveDIDDocumentWithInitialValue)
	s.Step(`^check for request success`, d.checkResponseIsSuccess)
	s.Step(`^client sends request to "([^"]*)" to create (\d+) DID documents using (\d+) concurrent requests$`, d.createDIDDocuments)
	s.Step(`^client sends request to "([^"]*)" to create (\d+) DID documents using (\d+) concurrent requests in the background$`, d.createDIDDocumentsAsync)
	s.Step(`^client sends request to "([^"]*)" to create (\d+) DID documents and update them with key ID "([^"]*)" using (\d+) concurrent requests$`, d.createAndUpdateDIDDocuments)
	s.Step(`^client sends request to "([^"]*)" to create (\d+) DID documents and update them with key ID "([^"]*)" using (\d+) concurrent requests in the background$`, d.createAndUpdateDIDDocumentsAsync)
	s.Step(`^client sends request to domains "([^"]*)" to create "([^"]*)" DID documents using "([^"]*)" concurrent requests storing the dids to file "([^"]*)"$`, d.createDIDDocumentsAndStoreDIDsToFile)
	s.Step(`^client sends request to domains "([^"]*)" to create "([^"]*)" DID documents using "([^"]*)" concurrent requests and a maximum of "([^"]*)" attempts, storing the dids to file "([^"]*)"$`, d.createDIDDocumentsWithRetriesAndStoreDIDsToFile)
	s.Step(`^client sends request to "([^"]*)" to verify the DID documents that were created$`, d.verifyDIDDocuments)
	s.Step(`^we wait up to "([^"]*)" for (\d+) DID documents to be created$`, d.waitForCreateDIDDocuments)
	s.Step(`^we wait up to "([^"]*)" for (\d+) DID documents to be created and updated$`, d.waitForCreateAndUpdateDIDDocuments)
	s.Step(`^client sends request to domains "([^"]*)" to verify the DID documents that were created from file "([^"]*)" with a maximum of "([^"]*)" attempts$`, d.verifyDIDDocumentsFromFile)
	s.Step(`^client sends request to "([^"]*)" to update the DID documents that were created with public key ID "([^"]*)" using (\d+) concurrent requests$`, d.updateDIDDocuments)
	s.Step(`^client sends request to "([^"]*)" to verify the DID documents that were updated with key "([^"]*)"$`, d.verifyUpdatedDIDDocuments)
	s.Step(`^client sends request to "([^"]*)" to verify the DID documents that were created and updated with key "([^"]*)"$`, d.verifyCreatedAndUpdatedDIDDocuments)
	s.Step(`^client sends request to "([^"]*)" to update the DID documents again with public key ID "([^"]*)" using (\d+) concurrent requests$`, d.updateDIDDocumentsAgain)
	s.Step(`^anchor origin for host "([^"]*)" is set to "([^"]*)"$`, d.setAnchorOrigin)
}
