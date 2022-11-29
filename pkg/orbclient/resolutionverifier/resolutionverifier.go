/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package resolutionverifier

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/trustbloc/sidetree-core-go/pkg/api/operation"
	"github.com/trustbloc/sidetree-core-go/pkg/api/protocol"
	"github.com/trustbloc/sidetree-core-go/pkg/canonicalizer"
	"github.com/trustbloc/sidetree-core-go/pkg/dochandler"
	"github.com/trustbloc/sidetree-core-go/pkg/document"
	"github.com/trustbloc/sidetree-core-go/pkg/processor"

	"github.com/trustbloc/orb/pkg/config"
	"github.com/trustbloc/orb/pkg/document/util"
	"github.com/trustbloc/orb/pkg/orbclient/protocol/nsprovider"
	"github.com/trustbloc/orb/pkg/orbclient/protocol/verprovider"
	"github.com/trustbloc/orb/pkg/protocolversion/clientregistry"
)

const v1 = "1.0"

// ResolutionVerifier verifies resolved documents.
type ResolutionVerifier struct {
	processor operationProcessor

	namespace string

	protocol       protocol.Client
	versions       []string
	currentVersion string

	methodContexts []string
	anchorOrigins  []string
	enableBase     bool
}

// operationProcessor is an interface which resolves the document based on operations provided.
type operationProcessor interface {
	Resolve(suffix string, opts ...document.ResolutionOption) (*protocol.ResolutionModel, error)
}

// Option is an option for document verifier.
type Option func(opts *ResolutionVerifier)

// New returns a new resolution verifier.
func New(namespace string, opts ...Option) (*ResolutionVerifier, error) {
	opStore := &noopOperationStore{}

	rv := &ResolutionVerifier{
		namespace: namespace,
		versions:  []string{v1},
	}

	// apply options
	for _, opt := range opts {
		opt(rv)
	}

	pc, err := getProtocolClient(namespace, rv.versions, rv.currentVersion, rv.methodContexts, rv.enableBase)
	if err != nil {
		return nil, fmt.Errorf("failed to create protocol client provider: %w", err)
	}

	rv.protocol = pc

	rv.processor = processor.New(namespace, opStore, pc)

	return rv, nil
}

// WithProtocolVersions sets optional client protocol versions.
func WithProtocolVersions(versions []string) Option {
	return func(opts *ResolutionVerifier) {
		opts.versions = versions
	}
}

// WithCurrentProtocolVersion sets optional current protocol versions.
// Defaults to the latest in the protocol versions list.
func WithCurrentProtocolVersion(version string) Option {
	return func(opts *ResolutionVerifier) {
		opts.currentVersion = version
	}
}

// WithMethodContext sets optional method contexts.
func WithMethodContext(methodContexts []string) Option {
	return func(opts *ResolutionVerifier) {
		opts.methodContexts = methodContexts
	}
}

// WithAnchorOrigins sets optional allowed anchor origins.
func WithAnchorOrigins(anchorOrigins []string) Option {
	return func(opts *ResolutionVerifier) {
		opts.anchorOrigins = anchorOrigins
	}
}

// WithEnableBase sets optional @base(JSON-LD directive).
func WithEnableBase(enabled bool) Option {
	return func(opts *ResolutionVerifier) {
		opts.enableBase = enabled
	}
}

func getProtocolClient(namespace string, versions []string, currentVersion string, methodContexts []string, enableBase bool) (protocol.Client, error) { //nolint:lll
	registry := clientregistry.New()

	var clientVersions []protocol.Version

	for _, version := range versions {
		cv, err := registry.CreateClientVersion(version, nil, &config.Sidetree{
			IncludePublishedOperations:   true,
			IncludeUnpublishedOperations: true,
			MethodContext:                methodContexts,
			EnableBase:                   enableBase,
		})
		if err != nil {
			return nil, fmt.Errorf("error creating client version [%s]: %w", version, err)
		}

		clientVersions = append(clientVersions, cv)
	}

	verProvider, err := verprovider.New(clientVersions, verprovider.WithCurrentProtocolVersion(currentVersion))
	if err != nil {
		return nil, err
	}

	nsProvider := nsprovider.New()
	nsProvider.Add(namespace, verProvider)

	pc, err := nsProvider.ForNamespace(namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get protocol client for namespace [%s]: %w", namespace, err)
	}

	return pc, nil
}

// Verify will verify provided resolution result against resolution result that is assembled
// from published and unpublished operations in provided resolution result.
func (r *ResolutionVerifier) Verify(input *document.ResolutionResult) error {
	// get operations from document metadata
	operations, err := getOperations(input.DocumentMetadata)
	if err != nil {
		return err
	}

	// resolve document using provided operations
	resolved, err := r.resolveDocument(input.Document.ID(), operations...)
	if err != nil {
		return fmt.Errorf("failed to resolve document with provided operations: %w", err)
	}

	// verify that assembled resolution result equals input resolution result
	err = checkResponses(input, resolved)
	if err != nil {
		return fmt.Errorf("failed to check input resolution result against assembled resolution result: %w", err)
	}

	return nil
}

func (r *ResolutionVerifier) resolveDocument(id string,
	ops ...*operation.AnchoredOperation) (*document.ResolutionResult, error) {
	pv, err := r.protocol.Current()
	if err != nil {
		return nil, err
	}

	suffix, err := util.GetSuffix(id)
	if err != nil {
		return nil, err
	}

	internalResult, err := r.processor.Resolve(suffix, document.WithAdditionalOperations(ops))
	if err != nil {
		return nil, err
	}

	var ti protocol.TransformationInfo
	if len(internalResult.PublishedOperations) > 0 {
		ti = dochandler.GetTransformationInfoForPublished(r.namespace, id, suffix, internalResult)
	} else {
		hint, err := util.GetHint(id, r.namespace, suffix)
		if err != nil {
			return nil, err
		}

		ti = dochandler.GetTransformationInfoForUnpublished(r.namespace, "", hint, suffix, "")
	}

	return pv.DocumentTransformer().TransformDocument(internalResult, ti)
}

func getOperations(metadata document.Metadata) ([]*operation.AnchoredOperation, error) {
	methodMetadata, err := util.GetMethodMetadata(metadata)
	if err != nil {
		return nil, err
	}

	unpublishedOps, err := getOperationsByKey(methodMetadata, document.UnpublishedOperationsProperty)
	if err != nil {
		return nil, fmt.Errorf("failed to get unpublished operations: %w", err)
	}

	publishedOps, err := getOperationsByKey(methodMetadata, document.PublishedOperationsProperty)
	if err != nil {
		return nil, fmt.Errorf("failed to get published operations: %w", err)
	}

	return append(publishedOps, unpublishedOps...), nil
}

func getOperationsByKey(methodMetadata map[string]interface{}, key string) ([]*operation.AnchoredOperation, error) {
	opsObj, ok := methodMetadata[key]
	if !ok {
		return nil, nil
	}

	opsBytes, err := json.Marshal(opsObj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal '%s'", key)
	}

	var ops []*operation.AnchoredOperation

	err = json.Unmarshal(opsBytes, &ops)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal '%s'", key)
	}

	return ops, nil
}

func checkResponses(input, resolved *document.ResolutionResult) error {
	err := equalDocuments(input.Document, resolved.Document)
	if err != nil {
		return err
	}

	return equalMetadata(input.DocumentMetadata, resolved.DocumentMetadata)
}

func equalDocuments(input, resolved document.Document) error {
	inputBytes, err := canonicalizer.MarshalCanonical(input)
	if err != nil {
		return fmt.Errorf("marshal canonical failed for input document: %w", err)
	}

	resolvedBytes, err := canonicalizer.MarshalCanonical(resolved)
	if err != nil {
		return fmt.Errorf("marshal canonical failed for resolved document: %w", err)
	}

	if !bytes.Equal(inputBytes, resolvedBytes) {
		return fmt.Errorf("input[%s] and resolved[%s] documents don't match",
			string(inputBytes), string(resolvedBytes))
	}

	return nil
}

func equalMetadata(input, resolved document.Metadata) error {
	inputMethodMetadata, err := util.GetMethodMetadata(input)
	if err != nil {
		return fmt.Errorf("unable to get input metadata: %w", err)
	}

	resolvedMethodMetadata, err := util.GetMethodMetadata(resolved)
	if err != nil {
		return fmt.Errorf("unable to get resolved metadata: %w", err)
	}

	if inputMethodMetadata[document.AnchorOriginProperty] != resolvedMethodMetadata[document.AnchorOriginProperty] {
		return fmt.Errorf("input[%s] and resolved[%s] anchor origins don't match",
			inputMethodMetadata[document.AnchorOriginProperty], resolvedMethodMetadata[document.AnchorOriginProperty])
	}

	if input[document.CanonicalIDProperty] != resolved[document.CanonicalIDProperty] {
		return fmt.Errorf("input[%s] and resolved[%s] canonical IDs don't match",
			input[document.CanonicalIDProperty], resolved[document.CanonicalIDProperty])
	}

	// before checking commitments check if document has been deactivated first
	inputDeactivated := getDeactivatedFlag(input)
	resolvedDeactivated := getDeactivatedFlag(resolved)

	if inputDeactivated != resolvedDeactivated {
		return fmt.Errorf("input[%t] and resolved[%t] deactivate flags don't match",
			inputDeactivated, resolvedDeactivated)
	}

	if inputDeactivated {
		// if document has been deactivated, then commitments will not be there -- nothing further to do
		return nil
	}

	err = checkCommitment(inputMethodMetadata, resolvedMethodMetadata, document.UpdateCommitmentProperty)
	if err != nil {
		return fmt.Errorf("input and resolved update commitments don't match: %w", err)
	}

	err = checkCommitment(inputMethodMetadata, resolvedMethodMetadata, document.RecoveryCommitmentProperty)
	if err != nil {
		return fmt.Errorf("input and resolved recovery commitments don't match: %w", err)
	}

	return nil
}

func checkCommitment(input, resolved map[string]interface{}, commitmentType string) error {
	ao, ok := input[commitmentType]
	if !ok {
		return fmt.Errorf("missing '%s' in input method metadata", commitmentType)
	}

	l, ok := resolved[commitmentType]
	if !ok {
		return fmt.Errorf("missing '%s' in resolved method metadata", commitmentType)
	}

	if ao != l {
		return fmt.Errorf("input value[%s] is different from resolved value[%s]", ao, l)
	}

	return nil
}

func getDeactivatedFlag(metadata document.Metadata) bool {
	obj, ok := metadata[document.DeactivatedProperty]
	if !ok {
		return false
	}

	deactivated, ok := obj.(bool)
	if !ok {
		return false
	}

	return deactivated
}

type noopOperationStore struct{}

func (s *noopOperationStore) Get(_ string) ([]*operation.AnchoredOperation, error) {
	return nil, nil
}

func (s *noopOperationStore) Put(_ []*operation.AnchoredOperation) error {
	return fmt.Errorf("should never be putting operations into store on client side - implement me")
}
