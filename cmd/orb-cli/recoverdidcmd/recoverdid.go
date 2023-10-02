/*
Copyright SecureKey Technologies Inc. All Rights Reserved.
SPDX-License-Identifier: Apache-2.0
*/

package recoverdidcmd

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"strconv"

	"github.com/hyperledger/aries-framework-go-ext/component/vdr/orb"
	"github.com/hyperledger/aries-framework-go-ext/component/vdr/sidetree/api"
	webcrypto "github.com/hyperledger/aries-framework-go/pkg/crypto"
	webkmscrypto "github.com/hyperledger/aries-framework-go/pkg/crypto/webkms"
	ariesdid "github.com/hyperledger/aries-framework-go/pkg/doc/did"
	vdrapi "github.com/hyperledger/aries-framework-go/pkg/framework/aries/api/vdr"
	"github.com/hyperledger/aries-framework-go/pkg/kms"
	"github.com/hyperledger/aries-framework-go/pkg/kms/webkms"
	"github.com/spf13/cobra"

	"github.com/trustbloc/orb/cmd/orb-cli/common"
	"github.com/trustbloc/orb/internal/pkg/cmdutil"
	"github.com/trustbloc/orb/internal/pkg/tlsutil"
)

const (
	didURIFlagName  = "did-uri"
	didURIEnvKey    = "ORB_CLI_DID_URI"
	didURIFlagUsage = "DID URI. " +
		" Alternatively, this can be set with the following environment variable: " + didURIEnvKey

	domainFlagName      = "domain"
	domainFileEnvKey    = "ORB_CLI_DOMAIN"
	domainFileFlagUsage = "URL to the did:trustbloc consortium's domain. " +
		" Alternatively, this can be set with the following environment variable: " + domainFileEnvKey

	kmsStoreEndpointFlagName  = "kms-store-endpoint"
	kmsStoreEndpointFlagUsage = "Remote KMS URL." +
		" Alternatively, this can be set with the following environment variable: " + kmsStoreEndpointEnvKey
	kmsStoreEndpointEnvKey = "ORB_CLI_KMS_STORE_ENDPOINT"

	sidetreeURLOpsFlagName  = "sidetree-url-operation"
	sidetreeURLOpsFlagUsage = "Comma-Separated list of sidetree url operation." +
		" Alternatively, this can be set with the following environment variable: " + sidetreeURLOpsEnvKey
	sidetreeURLOpsEnvKey = "ORB_CLI_SIDETREE_URL_OPERATION"

	sidetreeURLResFlagName  = "sidetree-url-resolution"
	sidetreeURLResFlagUsage = "Comma-Separated list of sidetree url resolution." +
		" Alternatively, this can be set with the following environment variable: " + sidetreeURLResEnvKey
	sidetreeURLResEnvKey = "ORB_CLI_SIDETREE_URL_Resolution"

	tlsSystemCertPoolFlagName  = "tls-systemcertpool"
	tlsSystemCertPoolFlagUsage = "Use system certificate pool." +
		" Possible values [true] [false]. Defaults to false if not set." +
		" Alternatively, this can be set with the following environment variable: " + tlsSystemCertPoolEnvKey
	tlsSystemCertPoolEnvKey = "ORB_CLI_TLS_SYSTEMCERTPOOL"

	tlsCACertsFlagName  = "tls-cacerts"
	tlsCACertsFlagUsage = "Comma-Separated list of ca certs path." +
		" Alternatively, this can be set with the following environment variable: " + tlsCACertsEnvKey
	tlsCACertsEnvKey = "ORB_CLI_TLS_CACERTS"

	sidetreeWriteTokenFlagName  = "sidetree-write-token"
	sidetreeWriteTokenEnvKey    = "ORB_CLI_SIDETREE_WRITE_TOKEN" //nolint: gosec
	sidetreeWriteTokenFlagUsage = "The sidetree write token " +
		" Alternatively, this can be set with the following environment variable: " + sidetreeWriteTokenEnvKey

	publicKeyFileFlagName  = "publickey-file"
	publicKeyFileEnvKey    = "ORB_CLI_PUBLICKEY_FILE"
	publicKeyFileFlagUsage = "publickey file include public keys for Orb DID " +
		" Alternatively, this can be set with the following environment variable: " + publicKeyFileEnvKey

	serviceFileFlagName = "service-file"
	serviceFileEnvKey   = "ORB_CLI_SERVICE_FILE"
	serviceFlagUsage    = "publickey file include services for Orb DID " +
		" Alternatively, this can be set with the following environment variable: " + serviceFileEnvKey

	signingKeyFlagName  = "signingkey"
	signingKeyEnvKey    = "ORB_CLI_SIGNINGKEY"
	signingKeyFlagUsage = "The private key PEM used for signing the recovery request." +
		" Alternatively, this can be set with the following environment variable: " + signingKeyEnvKey

	signingKeyFileFlagName  = "signingkey-file"
	signingKeyFileEnvKey    = "ORB_CLI_SIGNINGKEY_FILE"
	signingKeyFileFlagUsage = "The file that contains the private key" +
		" PEM used for signing the recovery request" +
		" Alternatively, this can be set with the following environment variable: " + signingKeyFileEnvKey

	signingKeyPasswordFlagName  = "signingkey-password"
	signingKeyPasswordEnvKey    = "ORB_CLI_SIGNINGKEY_PASSWORD" //nolint: gosec
	signingKeyPasswordFlagUsage = "signing key pem password. " +
		" Alternatively, this can be set with the following environment variable: " + signingKeyPasswordEnvKey

	signingKeyIDFlagName  = "signingkey-id"
	signingKeyIDEnvKey    = "ORB_CLI_SIGNINGKEY_ID"
	signingKeyIDFlagUsage = "The key id in kms" +
		" used for signing the recovery request." +
		" Alternatively, this can be set with the following environment variable: " + signingKeyIDEnvKey

	nextUpdateKeyFlagName  = "nextupdatekey"
	nextUpdateKeyEnvKey    = "ORB_CLI_NEXTUPDATEKEY"
	nextUpdateKeyFlagUsage = "The public key PEM used for validating the signature of the next update of the document." +
		" Alternatively, this can be set with the following environment variable: " + nextUpdateKeyEnvKey

	nextUpdateKeyFileFlagName  = "nextupdatekey-file"
	nextUpdateKeyFileEnvKey    = "ORB_CLI_NEXTUPDATEKEY_FILE"
	nextUpdateKeyFileFlagUsage = "The file that contains the public key" +
		" PEM used for validating the signature of the next update of the document. " +
		" Alternatively, this can be set with the following environment variable: " + nextUpdateKeyFileEnvKey

	nextUpdateKeyIDFlagName  = "nextupdatekey-id"
	nextUpdateKeyIDEnvKey    = "ORB_CLI_NEXTUPDATEKEY_ID" //nolint:gosec
	nextUpdateKeyIDFlagUsage = "The key id in kms" +
		" used for validating the signature of the next update of the document. " +
		" Alternatively, this can be set with the following environment variable: " + nextUpdateKeyIDEnvKey

	nextRecoveryKeyFlagName  = "nextrecoverykey"
	nextRecoveryKeyEnvKey    = "ORB_CLI_NEXTRECOVERYKEY"
	nextRecoveryKeyFlagUsage = "The public key PEM used for validating the" +
		" signature of the next recovery of the document." +
		" Alternatively, this can be set with the following environment variable: " + nextRecoveryKeyEnvKey

	nextRecoveryKeyFileFlagName  = "nextrecoverkey-file"
	nextRecoveryKeyFileEnvKey    = "ORB_CLI_NEXTRECOVERYKEY_FILE"
	nextRecoveryKeyFileFlagUsage = "The file that contains the public key" +
		" PEM used for validating the signature of the next recovery of the document. " +
		" Alternatively, this can be set with the following environment variable: " + nextRecoveryKeyFileEnvKey

	nextRecoveryKeyIDFlagName  = "nextrecoverkey-id"
	nextRecoveryKeyIDEnvKey    = "ORB_CLI_NEXTRECOVERYKEY_ID"
	nextRecoveryKeyIDFlagUsage = "The key id in kms" +
		" used for validating the signature of the next recovery of the document. " +
		" Alternatively, this can be set with the following environment variable: " + nextRecoveryKeyIDEnvKey

	didAnchorOriginFlagName  = "did-anchor-origin"
	didAnchorOriginEnvKey    = "ORB_CLI_DID_ANCHOR_ORIGIN"
	didAnchorOriginFlagUsage = "did anchor origin " +
		" Alternatively, this can be set with the following environment variable: " + didAnchorOriginEnvKey

	didAlsoKnownAsFlagName  = "did-also-known-as"
	didAlsoKnownAsFlagUsage = "Comma-separated list of also known as uris." +
		" Alternatively, this can be set with the following environment variable: " + didAlsoKnownAsEnvKey
	didAlsoKnownAsEnvKey = "ORB_CLI_DID_ALSO_KNOWN_AS"
)

// GetRecoverDIDCmd returns the Cobra recover did command.
func GetRecoverDIDCmd() *cobra.Command {
	recoverDIDCmd := recoverDIDCmd()

	createFlags(recoverDIDCmd)

	return recoverDIDCmd
}

func recoverDIDCmd() *cobra.Command { //nolint: funlen,gocognit,cyclop
	return &cobra.Command{
		Use:          "recover",
		Short:        "Recover orb DID",
		Long:         "Recover orb DID",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			rootCAs, err := getRootCAs(cmd)
			if err != nil {
				return err
			}

			didURI, err := cmdutil.GetUserSetVarFromString(cmd, didURIFlagName,
				didURIEnvKey, false)
			if err != nil {
				return err
			}

			sidetreeWriteToken := cmdutil.GetUserSetOptionalVarFromString(cmd, sidetreeWriteTokenFlagName,
				sidetreeWriteTokenEnvKey)

			didDoc, opts, err := recoverDIDOption(didURI, cmd)
			if err != nil {
				return err
			}

			httpClient := http.Client{Transport: &http.Transport{
				ForceAttemptHTTP2: true,
				TLSClientConfig:   &tls.Config{RootCAs: rootCAs, MinVersion: tls.VersionTLS12},
			}}

			kmsStoreURL := cmdutil.GetUserSetOptionalVarFromString(cmd, kmsStoreEndpointFlagName,
				kmsStoreEndpointEnvKey)

			var webKmsClient kms.KeyManager
			var webKmsCryptoClient webcrypto.Crypto

			if kmsStoreURL != "" {
				webKmsClient = webkms.New(kmsStoreURL, &httpClient)
				webKmsCryptoClient = webkmscrypto.New(kmsStoreURL, &httpClient)
			}

			var signingKey interface{}
			var signingKeyID string
			var signingKeyPK interface{}
			var nextUpdateKey interface{}
			var nextRecoveryKey interface{}

			if webKmsClient == nil { //nolint: nestif
				signingKey, err = common.GetKey(cmd, signingKeyFlagName, signingKeyEnvKey, signingKeyFileFlagName,
					signingKeyFileEnvKey, []byte(cmdutil.GetUserSetOptionalVarFromString(cmd, signingKeyPasswordFlagName,
						signingKeyPasswordEnvKey)), true)
				if err != nil {
					return err
				}

				nextUpdateKey, err = common.GetKey(cmd, nextUpdateKeyFlagName, nextUpdateKeyEnvKey, nextUpdateKeyFileFlagName,
					nextUpdateKeyFileEnvKey, nil, false)
				if err != nil {
					return err
				}

				nextRecoveryKey, err = common.GetKey(cmd, nextRecoveryKeyFlagName, nextRecoveryKeyEnvKey,
					nextRecoveryKeyFileFlagName, nextUpdateKeyFileEnvKey, nil, false)
				if err != nil {
					return err
				}
			} else {
				nextUpdateKey, err = common.GetPublicKeyFromKMS(cmd, nextUpdateKeyIDFlagName,
					nextUpdateKeyIDEnvKey, webKmsClient)
				if err != nil {
					return err
				}

				nextRecoveryKey, err = common.GetPublicKeyFromKMS(cmd, nextRecoveryKeyIDFlagName,
					nextRecoveryKeyIDEnvKey, webKmsClient)
				if err != nil {
					return err
				}

				signingKeyID, err = cmdutil.GetUserSetVarFromString(cmd, signingKeyIDFlagName,
					signingKeyIDEnvKey, false)
				if err != nil {
					return err
				}

				signingKeyID = fmt.Sprintf("%s/keys/%s", kmsStoreURL, signingKeyID)

				signingKeyPK, err = common.GetPublicKeyFromKMS(cmd, signingKeyIDFlagName,
					signingKeyIDEnvKey, webKmsClient)
				if err != nil {
					return err
				}
			}

			vdr, err := orb.New(&keyRetriever{
				nextUpdateKey:      nextUpdateKey,
				nextRecoveryKey:    nextRecoveryKey,
				signingKey:         signingKey,
				signingKeyID:       signingKeyID,
				webKmsCryptoClient: webKmsCryptoClient,
				signingKeyPK:       signingKeyPK,
			}, orb.WithAuthToken(sidetreeWriteToken),
				orb.WithDomain(cmdutil.GetUserSetOptionalVarFromString(cmd, domainFlagName, domainFileEnvKey)),
				orb.WithHTTPClient(&httpClient))
			if err != nil {
				return err
			}

			err = vdr.Update(didDoc, opts...)
			if err != nil {
				return fmt.Errorf("failed to recover did: %w", err)
			}

			fmt.Printf("successfully recoverd DID %s", didURI)

			return nil
		},
	}
}

func getSidetreeURL(cmd *cobra.Command) []vdrapi.DIDMethodOption {
	var opts []vdrapi.DIDMethodOption

	sidetreeURLOps := cmdutil.GetUserSetOptionalVarFromArrayString(cmd, sidetreeURLOpsFlagName,
		sidetreeURLOpsEnvKey)

	if len(sidetreeURLOps) > 0 {
		opts = append(opts, vdrapi.WithOption(orb.OperationEndpointsOpt, sidetreeURLOps))
	}

	sidetreeURLRes := cmdutil.GetUserSetOptionalVarFromArrayString(cmd, sidetreeURLResFlagName,
		sidetreeURLResEnvKey)

	if len(sidetreeURLRes) > 0 {
		opts = append(opts, vdrapi.WithOption(orb.ResolutionEndpointsOpt, sidetreeURLRes))
	}

	return opts
}

func recoverDIDOption(didID string, cmd *cobra.Command) (*ariesdid.Doc, []vdrapi.DIDMethodOption, error) {
	opts := getSidetreeURL(cmd)

	opts = append(opts, vdrapi.WithOption(orb.RecoverOpt, true))

	didDoc, err := getPublicKeys(cmd)
	if err != nil {
		return nil, nil, err
	}

	services, err := getServices(cmd)
	if err != nil {
		return nil, nil, err
	}

	didAnchorOrigin, err := cmdutil.GetUserSetVarFromString(cmd, didAnchorOriginFlagName,
		didAnchorOriginEnvKey, false)
	if err != nil {
		return nil, nil, err
	}

	opts = append(opts, vdrapi.WithOption(orb.AnchorOriginOpt, didAnchorOrigin))

	didDoc.ID = didID
	didDoc.Service = services

	alsoKnownAs := cmdutil.GetUserSetOptionalVarFromArrayString(cmd, didAlsoKnownAsFlagName,
		didAlsoKnownAsEnvKey)

	if len(alsoKnownAs) > 0 {
		didDoc.AlsoKnownAs = alsoKnownAs
	}

	return didDoc, opts, nil
}

func getServices(cmd *cobra.Command) ([]ariesdid.Service, error) {
	serviceFile := cmdutil.GetUserSetOptionalVarFromString(cmd, serviceFileFlagName,
		serviceFileEnvKey)

	var svc []ariesdid.Service

	if serviceFile != "" {
		services, err := common.GetServices(serviceFile)
		if err != nil {
			return nil, fmt.Errorf("failed to get services from file %w", err)
		}

		svc = append(svc, services...)
	}

	return svc, nil
}

func getPublicKeys(cmd *cobra.Command) (*ariesdid.Doc, error) {
	publicKeyFile := cmdutil.GetUserSetOptionalVarFromString(cmd, publicKeyFileFlagName,
		publicKeyFileEnvKey)

	if publicKeyFile != "" {
		return common.GetVDRPublicKeysFromFile(publicKeyFile)
	}

	return &ariesdid.Doc{}, nil
}

func getRootCAs(cmd *cobra.Command) (*x509.CertPool, error) {
	tlsSystemCertPoolString := cmdutil.GetUserSetOptionalVarFromString(cmd, tlsSystemCertPoolFlagName,
		tlsSystemCertPoolEnvKey)

	tlsSystemCertPool := false

	if tlsSystemCertPoolString != "" {
		var err error
		tlsSystemCertPool, err = strconv.ParseBool(tlsSystemCertPoolString)

		if err != nil {
			return nil, err
		}
	}

	tlsCACerts := cmdutil.GetUserSetOptionalVarFromArrayString(cmd, tlsCACertsFlagName,
		tlsCACertsEnvKey)

	return tlsutil.GetCertPool(tlsSystemCertPool, tlsCACerts)
}

func createFlags(startCmd *cobra.Command) {
	startCmd.Flags().StringP(didURIFlagName, "", "", didURIFlagUsage)
	startCmd.Flags().StringP(domainFlagName, "", "", domainFileFlagUsage)
	startCmd.Flags().StringP(tlsSystemCertPoolFlagName, "", "",
		tlsSystemCertPoolFlagUsage)
	startCmd.Flags().StringArrayP(tlsCACertsFlagName, "", []string{}, tlsCACertsFlagUsage)
	startCmd.Flags().StringP(sidetreeWriteTokenFlagName, "", "", sidetreeWriteTokenFlagUsage)
	startCmd.Flags().StringP(publicKeyFileFlagName, "", "", publicKeyFileFlagUsage)
	startCmd.Flags().StringP(serviceFileFlagName, "", "", serviceFlagUsage)
	startCmd.Flags().StringArrayP(sidetreeURLOpsFlagName, "", []string{}, sidetreeURLOpsFlagUsage)
	startCmd.Flags().StringArrayP(sidetreeURLResFlagName, "", []string{}, sidetreeURLResFlagUsage)
	startCmd.Flags().StringP(signingKeyFlagName, "", "", signingKeyFlagUsage)
	startCmd.Flags().StringP(signingKeyFileFlagName, "", "", signingKeyFileFlagUsage)
	startCmd.Flags().StringP(nextUpdateKeyFlagName, "", "", nextUpdateKeyFlagUsage)
	startCmd.Flags().StringP(nextUpdateKeyFileFlagName, "", "", nextUpdateKeyFileFlagUsage)
	startCmd.Flags().StringP(signingKeyPasswordFlagName, "", "", signingKeyPasswordFlagUsage)
	startCmd.Flags().StringP(nextRecoveryKeyFlagName, "", "", nextRecoveryKeyFlagUsage)
	startCmd.Flags().StringP(nextRecoveryKeyFileFlagName, "", "", nextRecoveryKeyFileFlagUsage)
	startCmd.Flags().StringP(didAnchorOriginFlagName, "", "", didAnchorOriginFlagUsage)
	startCmd.Flags().StringArrayP(didAlsoKnownAsFlagName, "", []string{}, didAlsoKnownAsFlagUsage)
	startCmd.Flags().String(kmsStoreEndpointFlagName, "", kmsStoreEndpointFlagUsage)
	startCmd.Flags().String(signingKeyIDFlagName, "", signingKeyIDFlagUsage)
	startCmd.Flags().String(nextUpdateKeyIDFlagName, "", nextUpdateKeyIDFlagUsage)
	startCmd.Flags().String(nextRecoveryKeyIDFlagName, "", nextRecoveryKeyIDFlagUsage)
}

type keyRetriever struct {
	nextUpdateKey      crypto.PublicKey
	nextRecoveryKey    crypto.PublicKey
	signingKey         crypto.PublicKey
	signingKeyID       string
	webKmsCryptoClient webcrypto.Crypto
	signingKeyPK       crypto.PublicKey
}

func (k *keyRetriever) GetNextRecoveryPublicKey(didID, commitment string) (crypto.PublicKey, error) {
	return k.nextRecoveryKey, nil
}

func (k *keyRetriever) GetNextUpdatePublicKey(didID, commitment string) (crypto.PublicKey, error) {
	return k.nextUpdateKey, nil
}

func (k *keyRetriever) GetSigner(didID string, ot orb.OperationType, commitment string) (api.Signer, error) {
	return common.NewSigner(k.signingKey, k.signingKeyID, k.webKmsCryptoClient, k.signingKeyPK), nil
}
