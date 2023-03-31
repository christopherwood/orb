/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package builder

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/hyperledger/aries-framework-go/pkg/doc/util"
	"github.com/hyperledger/aries-framework-go/pkg/doc/verifiable"

	"github.com/trustbloc/orb/pkg/activitypub/vocab"
)

const (
	// this context is preloaded by aries framework.
	vcContextURIV1 = "https://www.w3.org/2018/credentials/v1"

	typeVerifiableCredential = "VerifiableCredential" //nolint:gosec
	typeAnchorCredential     = "AnchorCredential"     //nolint:gosec
	typeAnchorLink           = "AnchorLink"

	relLinkset = "linkset"
)

// Params holds required parameters for building anchor credential.
type Params struct {
	Issuer string
	URL    string
}

// New returns new instance of anchor credential builder.
func New(params Params) (*Builder, error) {
	if err := verifyBuilderParams(params); err != nil {
		return nil, fmt.Errorf("failed to verify builder parameters: %w", err)
	}

	return &Builder{
		params: params,
	}, nil
}

// Builder implements building of anchor credential.
type Builder struct {
	params Params
}

// CredentialSubject contains the verifiable credential subject.
type CredentialSubject struct {
	HRef    string   `json:"href"`
	Profile string   `json:"profile"`
	Anchor  string   `json:"anchor"`
	Type    []string `json:"type"`
	Rel     string   `json:"rel"`
}

// Build will create and sign anchor credential.
func (b *Builder) Build(profile *url.URL, anchorHashlink, coreIndexHashlink string, context []string) (*verifiable.Credential, error) {
	id := b.params.URL + "/" + uuid.New().String()

	now := &util.TimeWrapper{Time: time.Now()}

	ctx := []string{vcContextURIV1, vocab.ContextActivityAnchors}

	ctx = append(ctx, context...)

	vc := &verifiable.Credential{
		Types:   []string{typeVerifiableCredential, typeAnchorCredential},
		Context: ctx,
		Subject: &CredentialSubject{
			Type:    []string{typeAnchorLink},
			Rel:     relLinkset,
			Profile: profile.String(),
			Anchor:  coreIndexHashlink,
			HRef:    anchorHashlink,
		},
		Issuer: verifiable.Issuer{
			ID: b.params.Issuer,
		},
		Issued: now,
		ID:     id,
	}

	return vc, nil
}

func verifyBuilderParams(params Params) error {
	if params.Issuer == "" {
		return errors.New("missing issuer")
	}

	if params.URL == "" {
		return errors.New("missing URL")
	}

	return nil
}
