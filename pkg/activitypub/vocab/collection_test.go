/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package vocab

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/trustbloc/sidetree-go/pkg/canonicalizer"

	"github.com/trustbloc/orb/pkg/internal/testutil"
)

var service1Inbox = testutil.MustParseURL("https://org1.com/services/service1/inbox")

func TestNilCollection(t *testing.T) {
	var coll *CollectionType

	require.Nil(t, coll.Current())
	require.Nil(t, coll.First())
	require.Nil(t, coll.Last())
	require.Empty(t, coll.Items())
	require.Zero(t, coll.TotalItems())
}

func TestCollectionMarshal(t *testing.T) {
	first := testutil.MustParseURL("https://org1.com/services/service1/inbox?page=true")
	last := testutil.MustParseURL("https://org1.com/services/service1/inbox?page=true&end=true")
	current := testutil.MustParseURL("https://org1.com/services/service1/inbox?page=2")
	txn1 := testutil.MustParseURL("https://org1.com/transactions/txn1")
	txn2 := testutil.MustParseURL("https://org1.com/transactions/txn2")

	t.Run("Marshal", func(t *testing.T) {
		items := []*ObjectProperty{
			NewObjectProperty(WithIRI(txn1)),
			NewObjectProperty(WithIRI(txn2)),
		}

		coll := NewCollection(items,
			WithContext(ContextActivityStreams),
			WithID(service1Inbox), WithTotalItems(100),
			WithFirst(first), WithLast(last), WithCurrent(current))

		bytes, err := canonicalizer.MarshalCanonical(coll)
		require.NoError(t, err)
		t.Log(string(bytes))

		require.Equal(t, testutil.GetCanonical(t, jsonCollection), string(bytes))
	})

	t.Run("Unmarshal", func(t *testing.T) {
		c := &CollectionType{}
		require.NoError(t, json.Unmarshal([]byte(jsonCollection), c))
		require.Equal(t, service1Inbox.String(), c.ID().String())

		context := c.Context()
		require.NotNil(t, context)
		context.Contains(ContextActivityStreams)

		curr := c.Current()
		require.NotNil(t, curr)
		require.Equal(t, current.String(), curr.String())

		frst := c.First()
		require.NotNil(t, frst)
		require.Equal(t, first.String(), frst.String())

		lst := c.Last()
		require.NotNil(t, lst)
		require.Equal(t, last.String(), lst.String())

		require.Equal(t, 100, c.TotalItems())

		items := c.Items()
		require.Len(t, items, 2)

		item := items[0]
		require.NotNil(t, item)
		iri := item.IRI()
		require.NotNil(t, iri)
		require.Equal(t, txn1.String(), iri.String())

		item = items[1]
		require.NotNil(t, item)
		iri = item.IRI()
		require.NotNil(t, iri)
		require.Equal(t, txn2.String(), iri.String())
	})
}

func TestOrderedCollectionMarshal(t *testing.T) {
	first := testutil.MustParseURL("https://org1.com/services/service1/inbox?page=true")
	last := testutil.MustParseURL("https://org1.com/services/service1/inbox?page=true&end=true")
	current := testutil.MustParseURL("https://org1.com/services/service1/inbox?page=2")
	txn1 := testutil.MustParseURL("https://org1.com/transactions/txn1")
	txn2 := testutil.MustParseURL("https://org1.com/transactions/txn2")

	t.Run("Marshal", func(t *testing.T) {
		items := []*ObjectProperty{
			NewObjectProperty(WithIRI(txn1)),
			NewObjectProperty(WithIRI(txn2)),
		}

		coll := NewOrderedCollection(items,
			WithContext(ContextActivityStreams),
			WithID(service1Inbox), WithTotalItems(100),
			WithFirst(first), WithLast(last), WithCurrent(current))

		bytes, err := canonicalizer.MarshalCanonical(coll)
		require.NoError(t, err)
		t.Log(string(bytes))

		require.Equal(t, testutil.GetCanonical(t, jsonOrderedCollection), string(bytes))
	})

	t.Run("Unmarshal", func(t *testing.T) {
		c := &OrderedCollectionType{}
		require.NoError(t, json.Unmarshal([]byte(jsonOrderedCollection), c))
		require.Equal(t, service1Inbox.String(), c.ID().String())

		context := c.Context()
		require.NotNil(t, context)
		context.Contains(ContextActivityStreams)

		curr := c.Current()
		require.NotNil(t, curr)
		require.Equal(t, current.String(), curr.String())

		frst := c.First()
		require.NotNil(t, frst)
		require.Equal(t, first.String(), frst.String())

		lst := c.Last()
		require.NotNil(t, lst)
		require.Equal(t, last.String(), lst.String())

		require.Equal(t, 100, c.TotalItems())

		items := c.Items()
		require.Len(t, items, 2)

		item := items[0]
		require.NotNil(t, item)
		iri := item.IRI()
		require.NotNil(t, iri)
		require.Equal(t, txn1.String(), iri.String())

		item = items[1]
		require.NotNil(t, item)
		iri = item.IRI()
		require.NotNil(t, iri)
		require.Equal(t, txn2.String(), iri.String())
	})
}

const (
	jsonCollection = `{
    "@context": "https://www.w3.org/ns/activitystreams",
    "id": "https://org1.com/services/service1/inbox",
    "type": "Collection",
    "totalItems": 100,
    "current": "https://org1.com/services/service1/inbox?page=2",
    "first": "https://org1.com/services/service1/inbox?page=true",
    "last": "https://org1.com/services/service1/inbox?page=true&end=true",
    "items": [
      "https://org1.com/transactions/txn1",
      "https://org1.com/transactions/txn2"
    ]
  }`

	jsonOrderedCollection = `{
    "@context": "https://www.w3.org/ns/activitystreams",
    "id": "https://org1.com/services/service1/inbox",
    "type": "OrderedCollection",
    "totalItems": 100,
    "current": "https://org1.com/services/service1/inbox?page=2",
    "first": "https://org1.com/services/service1/inbox?page=true",
    "last": "https://org1.com/services/service1/inbox?page=true&end=true",
    "orderedItems": [
      "https://org1.com/transactions/txn1",
      "https://org1.com/transactions/txn2"
    ]
  }`
)
