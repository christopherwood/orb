/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package common

import (
	"net/url"

	"github.com/trustbloc/sidetree-go/pkg/api/operation"

	"github.com/trustbloc/orb/pkg/anchor/graph"
)

// OperationStore interface to access operation store.
type OperationStore interface {
	Get(suffix string) ([]*operation.AnchoredOperation, error)
	Put(ops []*operation.AnchoredOperation) error
}

// AnchorGraph interface to access did anchors.
type AnchorGraph interface {
	GetDidAnchors(cid, suffix string) ([]graph.Anchor, error)
}

// CASResolver interface to resolve cid. Returns the content and a hashlink to the local CAS store.
type CASResolver interface {
	Resolve(webCASURL *url.URL, cid string, data []byte) ([]byte, string, error)
}

// CASReader interface to read from content addressable storage.
type CASReader interface {
	Read(key string) ([]byte, error)
}
