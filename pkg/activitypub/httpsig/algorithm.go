/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package httpsig

import (
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"

	ariesverifier "github.com/hyperledger/aries-framework-go/pkg/doc/signature/verifier"
	httpsig "github.com/igor-pavlenko/httpsignatures-go"
	"github.com/trustbloc/logutil-go/pkg/log"

	logfields "github.com/trustbloc/orb/internal/pkg/log"
)

const orbHTTPSigAlgorithm = "Sign"

type keyResolver interface {
	// Resolve returns the public key bytes and the type of public key for the given key ID.
	Resolve(keyID string) (*ariesverifier.PublicKey, error)
}

// SignatureHashAlgorithm is a custom httpsignatures.SignatureHashAlgorithm that uses KMS to sign HTTP requests.
type SignatureHashAlgorithm struct {
	Crypto      crypto
	KMS         keyManager
	keyResolver keyResolver
	keyID       string
}

// NewSignerAlgorithm returns a new SignatureHashAlgorithm which uses KMS to sign HTTP requests.
func NewSignerAlgorithm(c crypto, km keyManager, keyID string) *SignatureHashAlgorithm {
	return &SignatureHashAlgorithm{
		Crypto: c,
		KMS:    km,
		keyID:  keyID,
	}
}

// NewVerifierAlgorithm returns a new SignatureHashAlgorithm which is used to verify the signature
// in the HTTP request header.
func NewVerifierAlgorithm(c crypto, km keyManager, keyResolver keyResolver) *SignatureHashAlgorithm {
	return &SignatureHashAlgorithm{
		Crypto:      c,
		KMS:         km,
		keyResolver: keyResolver,
	}
}

// Algorithm returns this algorithm's name.
func (a *SignatureHashAlgorithm) Algorithm() string {
	return orbHTTPSigAlgorithm
}

// Create signs data with the secret.
func (a *SignatureHashAlgorithm) Create(secret httpsig.Secret, data []byte) ([]byte, error) {
	kh, err := a.KMS.Get(a.keyID)
	if err != nil {
		return nil, fmt.Errorf("get KMS key handle: %w", err)
	}

	logger.Debug("Got key handle. Signing ...", logfields.WithKeyID(secret.KeyID))

	sig, err := a.Crypto.Sign(data, kh)
	if err != nil {
		return nil, fmt.Errorf("sign data: %w", err)
	}

	logger.Debug("... successfully signed data with KMS", logfields.WithKeyID(a.keyID))

	return sig, nil
}

// Verify verifies the signature over data with the secret.
func (a *SignatureHashAlgorithm) Verify(secret httpsig.Secret, data, signature []byte) error {
	pubKey, err := a.keyResolver.Resolve(secret.KeyID)
	if err != nil {
		return fmt.Errorf("resolve key %s: %w", secret.KeyID, err)
	}

	logger.Debug("Got public key", logfields.WithKeyType(pubKey.Type), logfields.WithKeyID(secret.KeyID))

	switch {
	case strings.HasPrefix(pubKey.Type, "Ed25519"):
		return ariesverifier.NewEd25519SignatureVerifier().Verify(pubKey, data, signature)
	case pubKey.Type == "P-256":
		return ariesverifier.NewECDSAES256SignatureVerifier().Verify(pubKey, data, signature)
	case pubKey.Type == "P-384":
		return ariesverifier.NewECDSAES384SignatureVerifier().Verify(pubKey, data, signature)
	case pubKey.Type == "P-521x":
		return ariesverifier.NewECDSAES521SignatureVerifier().Verify(pubKey, data, signature)
	}

	return fmt.Errorf("key not supported %s", pubKey.Type)
}

// KeyResolver resolves the public key for an ActivityPub actor.
type KeyResolver struct {
	pubKeyRetriever actorRetriever
}

// NewKeyResolver returns a new KeyResolver.
func NewKeyResolver(actorRetriever actorRetriever) *KeyResolver {
	return &KeyResolver{pubKeyRetriever: actorRetriever}
}

// Resolve returns the public key for the given key ID.
func (r *KeyResolver) Resolve(keyID string) (*ariesverifier.PublicKey, error) {
	keyIRI, err := url.Parse(keyID)
	if err != nil {
		logger.Error("Error parsing public key", logfields.WithKeyID(keyID), log.WithError(err))

		return nil, fmt.Errorf("parse key IRI [%s]: %w", keyID, err)
	}

	logger.Debug("Retrieving public key", logfields.WithKeyIRI(keyIRI))

	pubKey, err := r.pubKeyRetriever.GetPublicKey(keyIRI)
	if err != nil {
		logger.Error("Error retrieving public key", logfields.WithKeyIRI(keyIRI), log.WithError(err))

		return nil, fmt.Errorf("retrieve public key for ID [%s]: %w", keyID, err)
	}

	block, _ := pem.Decode([]byte(pubKey.PublicKeyPem()))
	if block == nil {
		return nil, fmt.Errorf("invalid public key for ID [%s]: nil block", keyID)
	}

	return &ariesverifier.PublicKey{
		Type:  block.Type,
		Value: block.Bytes,
	}, nil
}

// SecretRetriever implements a custom key retriever to be used with the HTTP signature library.
type SecretRetriever struct{}

// Get returns a 'secret' that directs the HTTP signature library to use the custom SignatureHashAlgorithm above.
func (r *SecretRetriever) Get(keyID string) (httpsig.Secret, error) {
	return httpsig.Secret{
		KeyID:     keyID,
		Algorithm: orbHTTPSigAlgorithm,
	}, nil
}
