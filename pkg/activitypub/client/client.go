/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/bluele/gcache"
	"github.com/hyperledger/aries-framework-go/pkg/doc/verifiable"
	"github.com/hyperledger/aries-framework-go/pkg/kms"
	"github.com/trustbloc/logutil-go/pkg/log"
	"go.opentelemetry.io/otel/trace"

	logfields "github.com/trustbloc/orb/internal/pkg/log"
	"github.com/trustbloc/orb/pkg/activitypub/client/transport"
	"github.com/trustbloc/orb/pkg/activitypub/vocab"
	discoveryrest "github.com/trustbloc/orb/pkg/discovery/endpoint/restapi"
	docutil "github.com/trustbloc/orb/pkg/document/util"
	orberrors "github.com/trustbloc/orb/pkg/errors"
	"github.com/trustbloc/orb/pkg/observability/tracing"
	cryptoutil "github.com/trustbloc/orb/pkg/util"
)

var logger = log.New("activitypub_client")

const (
	defaultCacheSize       = 100
	defaultCacheExpiration = time.Minute
)

// ErrNotFound is returned when the object is not found or the iterator has reached the end.
var ErrNotFound = fmt.Errorf("not found")

// Order is the order in which activities are returned.
type Order string

const (
	// Forward indicates that activities should be returned in the same order that they were retrieved
	// from the REST endpoint.
	Forward Order = "forward"
	// Reverse indicates that activities should be returned in reverse order that they were retrieved
	// from the REST endpoint..
	Reverse Order = "reverse"
)

// ReferenceIterator iterates over the references in a result set.
type ReferenceIterator interface {
	Next() (*url.URL, error)
	TotalItems() int
}

// ActivityIterator iterates over the activities in a result set.
type ActivityIterator interface {
	// Next returns the next activity or the ErrNotFound error if no more items are available.
	Next() (*vocab.ActivityType, error)
	// NextPage advances to the next page. If there are no more pages then an ErrNotFound error is returned.
	NextPage() (*url.URL, error)
	// SetNextIndex sets the index of the next activity within the current page that Next will return.
	SetNextIndex(int)
	// TotalItems returns the total number of items available at the moment the iterator was created.
	// This value remains constant throughout the lifetime of the iterator.
	TotalItems() int
	// CurrentPage returns the ID of the current page that the iterator is processing.
	CurrentPage() *url.URL
	// NextIndex returns the next index of the current page that will be processed. This function does not
	// advance the iterator.
	NextIndex() int
}

type httpTransport interface {
	Get(ctx context.Context, req *transport.Request) (*http.Response, error)
}

type serviceResolver interface {
	ResolveHostMetaLink(uri, linkType string) (string, error)
}

// Config contains configuration parameters for the client.
type Config struct {
	CacheSize       int
	CacheExpiration time.Duration
}

// Client implements an ActivityPub client which retrieves ActivityPub objects (such as actors, activities,
// and collections) from remote sources.
type Client struct {
	httpTransport

	actorCache     gcache.Cache
	publicKeyCache gcache.Cache
	fetchPublicKey verifiable.PublicKeyFetcher
	resolver       serviceResolver
	tracer         trace.Tracer
}

// New returns a new ActivityPub client.
func New(cfg Config, t httpTransport, fetchPublicKey verifiable.PublicKeyFetcher, resolver serviceResolver) *Client {
	c := &Client{
		httpTransport:  t,
		fetchPublicKey: fetchPublicKey,
		resolver:       resolver,
		tracer:         tracing.Tracer(tracing.SubsystemActivityPub),
	}

	cacheSize := cfg.CacheSize

	if cacheSize == 0 {
		cacheSize = defaultCacheSize
	}

	cacheExpiration := cfg.CacheExpiration

	if cacheExpiration == 0 {
		cacheExpiration = defaultCacheExpiration
	}

	logger.Debug("Creating actor cache", logfields.WithSize(cacheSize), logfields.WithCacheExpiration(cacheExpiration))

	c.actorCache = gcache.New(cacheSize).ARC().
		Expiration(cacheExpiration).
		LoaderFunc(func(i interface{}) (interface{}, error) {
			return c.loadActor(i.(string)) //nolint:forcetypeassert
		}).Build()

	c.publicKeyCache = gcache.New(cacheSize).ARC().
		Expiration(cacheExpiration).
		LoaderFunc(func(i interface{}) (interface{}, error) {
			return c.loadPublicKey(i.(string)) //nolint:forcetypeassert
		}).Build()

	return c
}

// GetActor retrieves the actor at the given IRI.
//
//nolint:interfacer
func (c *Client) GetActor(actorIRI *url.URL) (*vocab.ActorType, error) {
	result, err := c.actorCache.Get(actorIRI.String())
	if err != nil {
		logger.Debug("Got error retrieving actor from cache", logfields.WithActorIRI(actorIRI), log.WithError(err))

		return nil, err
	}

	return result.(*vocab.ActorType), nil //nolint:forcetypeassert
}

func (c *Client) loadActor(actorIRI string) (*vocab.ActorType, error) {
	logger.Debug("Cache miss. Resolving actor for target.", logfields.WithTarget(actorIRI))

	resolvedActorIRI, err := c.resolver.ResolveHostMetaLink(actorIRI, discoveryrest.ActivityJSONType)
	if err != nil {
		return nil, fmt.Errorf("resolve host meta-link: %w", err)
	}

	logger.Debug("Resolved URL for actor", logfields.WithTarget(actorIRI), logfields.WithActorID(resolvedActorIRI))

	u, err := url.Parse(resolvedActorIRI)
	if err != nil {
		return nil, fmt.Errorf("parse actor IRI [%s]: %w", resolvedActorIRI, err)
	}

	ctx, span := c.tracer.Start(context.Background(), "load actor to cache")
	defer span.End()

	respBytes, err := c.get(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("error reading response from %s: %w", actorIRI, err)
	}

	logger.Debugc(ctx, "Got response from actor", logfields.WithActorIRI(u), log.WithResponse(respBytes))

	actor := &vocab.ActorType{}

	err = json.Unmarshal(respBytes, actor)
	if err != nil {
		return nil, fmt.Errorf("invalid actor in response from %s: %w", actorIRI, err)
	}

	return actor, nil
}

// GetPublicKey retrieves the public key at the given IRI.
//
//nolint:interfacer,forcetypeassert
func (c *Client) GetPublicKey(keyIRI *url.URL) (*vocab.PublicKeyType, error) {
	result, err := c.publicKeyCache.Get(keyIRI.String())
	if err != nil {
		logger.Debug("Got error retrieving public key from cache", logfields.WithKeyIRI(keyIRI), log.WithError(err))

		return nil, err
	}

	return result.(*vocab.PublicKeyType), nil
}

func (c *Client) loadPublicKey(keyIRI string) (*vocab.PublicKeyType, error) {
	logger.Debug("Cache miss. Loading public key", logfields.WithKeyID(keyIRI))

	if docutil.IsDID(keyIRI) {
		return c.resolvePublicKeyFromDID(keyIRI)
	}

	keyURL, err := url.Parse(keyIRI)
	if err != nil {
		return nil, fmt.Errorf("parse key IRI [%s]: %w", keyIRI, err)
	}

	ctx, span := c.tracer.Start(context.Background(), "load public key to cache")
	defer span.End()

	respBytes, err := c.get(ctx, keyURL)
	if err != nil {
		return nil, fmt.Errorf("error reading response from %s: %w", keyIRI, err)
	}

	logger.Debugc(ctx, "Got public key", logfields.WithKeyID(keyIRI), log.WithResponse(respBytes))

	pubKey := &vocab.PublicKeyType{}

	err = json.Unmarshal(respBytes, pubKey)
	if err != nil {
		return nil, fmt.Errorf("invalid public key in response from %s: %w", keyIRI, err)
	}

	return pubKey, nil
}

// GetReferences returns an iterator that reads all references at the given IRI. The IRI either resolves
// to an ActivityPub actor, collection or ordered collection.
func (c *Client) GetReferences(ctx context.Context, iri *url.URL) (ReferenceIterator, error) {
	respBytes, err := c.get(ctx, iri)
	if err != nil {
		return nil, fmt.Errorf("error reading response from %s: %w", iri, err)
	}

	logger.Debugc(ctx, "Got references", logfields.WithURI(iri), log.WithResponse(respBytes))

	objProps, firstPage, _, totalItems, err := unmarshalCollection(respBytes)
	if err != nil {
		return nil, fmt.Errorf("error unmarsalling response from %s: %w", iri, err)
	}

	items := make([]*url.URL, len(objProps))

	for i, prop := range objProps {
		items[i] = prop.IRI()
	}

	return newReferenceIterator(ctx, items, firstPage, totalItems, c.get), nil
}

// GetActivities returns an iterator that reads activities at the given IRI. The IRI may reference a
// Collection, OrderedCollection, CollectionPage, or OrderedCollectionPage.
func (c *Client) GetActivities(ctx context.Context, iri *url.URL, order Order) (ActivityIterator, error) {
	respBytes, err := c.get(ctx, iri)
	if err != nil {
		return nil, fmt.Errorf("error reading response from %s: %w", iri, err)
	}

	logger.Debugc(ctx, "Got activities", logfields.WithURI(iri), log.WithResponse(respBytes))

	obj := &vocab.ObjectType{}

	err = json.Unmarshal(respBytes, &obj)
	if err != nil {
		return nil, err
	}

	switch {
	case obj.Type().IsAny(vocab.TypeCollection, vocab.TypeOrderedCollection):
		return c.activityIteratorFromCollection(ctx, respBytes, order)
	case obj.Type().IsAny(vocab.TypeCollectionPage, vocab.TypeOrderedCollectionPage):
		return c.activityIteratorFromCollectionPage(ctx, respBytes, order)
	default:
		return nil, fmt.Errorf("invalid collection type %s", obj.Type())
	}
}

func (c *Client) activityIteratorFromCollection(ctx context.Context, collBytes []byte, order Order) (ActivityIterator, error) {
	_, first, last, totalItems, err := unmarshalCollection(collBytes)
	if err != nil {
		return nil, fmt.Errorf("unmarsal collection: %w", err)
	}

	switch order {
	case Forward:
		logger.Debugc(ctx, "Creating forward activity iterator",
			logfields.WithNextIRI(first), logfields.WithTotal(totalItems))

		return newForwardActivityIterator(ctx, nil, nil, first, totalItems, c.get), nil
	case Reverse:
		logger.Debugc(ctx, "Creating reverse activity iterator",
			logfields.WithNextIRI(last), logfields.WithTotal(totalItems))

		return newReverseActivityIterator(ctx, nil, nil, last, totalItems, c.get), nil
	default:
		return nil, fmt.Errorf("invalid order [%s]", order)
	}
}

func (c *Client) activityIteratorFromCollectionPage(ctx context.Context, collBytes []byte, order Order) (ActivityIterator, error) {
	page, err := unmarshalCollectionPage(collBytes)
	if err != nil {
		return nil, fmt.Errorf("unmarsal collection page: %w", err)
	}

	activities := make([]*vocab.ActivityType, len(page.items))

	for i, prop := range page.items {
		activities[i] = prop.Activity()
	}

	switch order {
	case Forward:
		logger.Debugc(ctx, "Creating forward activity iterator",
			logfields.WithCurrentIRI(page.current), logfields.WithSize(len(activities)), logfields.WithTotal(page.totalItems))

		return newForwardActivityIterator(ctx, activities, page.current, page.next, page.totalItems, c.get), nil
	case Reverse:
		logger.Debugc(ctx, "Creating reverse activity iterator",
			logfields.WithCurrentIRI(page.current), logfields.WithSize(len(activities)), logfields.WithTotal(page.totalItems))

		return newReverseActivityIterator(ctx, activities, page.current, page.prev, page.totalItems, c.get), nil
	default:
		return nil, fmt.Errorf("invalid order [%s]", order)
	}
}

func (c *Client) get(ctx context.Context, iri *url.URL) ([]byte, error) {
	resp, err := c.Get(ctx, transport.NewRequest(iri,
		transport.WithHeader(transport.AcceptHeader, transport.ActivityStreamsContentType)))
	if err != nil {
		return nil, orberrors.NewTransientf("transient http error: request to %s failed: %w",
			iri, err)
	}

	defer func() {
		if e := resp.Body.Close(); e != nil {
			log.CloseResponseBodyError(logger, e)
		}
	}()

	logger.Debugc(ctx, "Got response code", logfields.WithRequestURL(iri), log.WithHTTPStatus(resp.StatusCode))

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusInternalServerError {
			return nil, orberrors.NewTransientf("transient http error: status code %d from %s",
				resp.StatusCode, iri)
		}

		return nil, fmt.Errorf("request to %s returned status code %d", iri, resp.StatusCode)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, orberrors.NewTransientf("transient http error: read response body from %s: %w",
			iri, err)
	}

	return respBytes, nil
}

func (c *Client) resolvePublicKeyFromDID(keyIRI string) (*vocab.PublicKeyType, error) {
	did, keyID, err := docutil.ParseKeyURI(keyIRI)
	if err != nil {
		return nil, err
	}

	pubKey, err := c.fetchPublicKey(did, keyID)
	if err != nil {
		return nil, fmt.Errorf("fetch public key - DID [%s], KeyID [%s]: %w", did, keyID, err)
	}

	var publicKeyBytes []byte

	var keyType kms.KeyType

	if pubKey.JWK == nil {
		publicKeyBytes = pubKey.Value
		keyType = kms.KeyType(pubKey.Type)
	} else {
		keyType, err = pubKey.JWK.KeyType()
		if err != nil {
			return nil, fmt.Errorf("get key type from JWK for [%s]: %w", keyIRI, err)
		}

		publicKeyBytes, err = pubKey.JWK.PublicKeyBytes()
		if err != nil {
			return nil, fmt.Errorf("get public key bytes from JWK for [%s]: %w", keyIRI, err)
		}
	}

	pemBytes, err := cryptoutil.EncodePublicKeyToPEM(publicKeyBytes, keyType)
	if err != nil {
		return nil, fmt.Errorf("convert public key to PEM - DID [%s], KeyID [%s]: %w", did, keyID, err)
	}

	id, err := url.Parse(keyIRI)
	if err != nil {
		return nil, fmt.Errorf("parse URL [%s]: %w", keyIRI, err)
	}

	owner, err := url.Parse(did)
	if err != nil {
		return nil, fmt.Errorf("parse URL [%s]: %w", did, err)
	}

	return vocab.NewPublicKey(
		vocab.WithID(id),
		vocab.WithOwner(owner),
		vocab.WithPublicKeyPem(string(pemBytes)),
	), nil
}

type getFunc func(ctx context.Context, iri *url.URL) ([]byte, error)

type referenceIterator struct {
	ctx          context.Context
	totalItems   int
	currentItems []*url.URL
	currentIndex int
	nextPage     *url.URL
	get          getFunc
}

func newReferenceIterator(ctx context.Context, items []*url.URL, nextPage *url.URL, totalItems int, retrieve getFunc) *referenceIterator {
	return &referenceIterator{
		ctx:          ctx,
		currentItems: items,
		totalItems:   totalItems,
		nextPage:     nextPage,
		get:          retrieve,
		currentIndex: 0,
	}
}

func (it *referenceIterator) Next() (*url.URL, error) {
	if it.currentIndex >= len(it.currentItems) {
		err := it.getNextPage()
		if err != nil {
			return nil, err
		}
	}

	item := it.currentItems[it.currentIndex]

	it.currentIndex++

	return item, nil
}

func (it *referenceIterator) TotalItems() int {
	return it.totalItems
}

func (it *referenceIterator) getNextPage() error {
	if it.nextPage == nil {
		return ErrNotFound
	}

	logger.Debug("Retrieving next page", logfields.WithNextIRI(it.nextPage))

	respBytes, err := it.get(it.ctx, it.nextPage)
	if err != nil {
		return fmt.Errorf("get references from %s: %w", it.nextPage, err)
	}

	logger.Debug("Got response", logfields.WithRequestURL(it.nextPage), log.WithResponse(respBytes))

	page, err := unmarshalCollectionPage(respBytes)
	if err != nil {
		return err
	}

	var refs []*url.URL

	for _, item := range page.items {
		if item.IRI() != nil {
			logger.Debug("Adding target to the recipient list", logfields.WithTarget(item.IRI().String()))

			refs = append(refs, item.IRI())
		} else {
			logger.Warn("expecting IRI item for collection but got other type", logfields.WithType(item.Type().String()))
		}
	}

	it.currentItems = refs
	it.currentIndex = 0
	it.nextPage = page.next

	if len(it.currentItems) == 0 {
		return ErrNotFound
	}

	return nil
}

type getNextIRIFunc func(next, prev *url.URL) *url.URL

type appendFunc func(activities []*vocab.ActivityType, activity *vocab.ActivityType) []*vocab.ActivityType

type activityIterator struct {
	ctx            context.Context
	currentItems   []*vocab.ActivityType
	currentPage    *url.URL
	nextPage       *url.URL
	totalItems     int
	currentIndex   int
	numProcessed   int
	get            getFunc
	getNext        getNextIRIFunc
	appendActivity appendFunc
}

func newActivityIterator(ctx context.Context, items []*vocab.ActivityType, currentPage, nextPage *url.URL, totalItems int,
	get getFunc, getNext getNextIRIFunc, appendActivity appendFunc) *activityIterator {
	return &activityIterator{
		ctx:            ctx,
		currentItems:   items,
		currentPage:    currentPage,
		nextPage:       nextPage,
		totalItems:     totalItems,
		get:            get,
		getNext:        getNext,
		appendActivity: appendActivity,
	}
}

func (it *activityIterator) CurrentPage() *url.URL {
	return it.currentPage
}

func (it *activityIterator) SetNextIndex(index int) {
	it.numProcessed += index - it.currentIndex
	it.currentIndex = index
}

func (it *activityIterator) NextIndex() int {
	return it.currentIndex
}

func (it *activityIterator) NextPage() (*url.URL, error) {
	unprocessedCount := len(it.currentItems) - it.currentIndex

	if err := it.getNextPage(); err != nil {
		if errors.Is(err, ErrNotFound) {
			it.numProcessed += unprocessedCount
		}

		return nil, err
	}

	it.numProcessed += unprocessedCount

	return it.CurrentPage(), nil
}

func (it *activityIterator) Next() (*vocab.ActivityType, error) {
	if it.numProcessed >= it.totalItems {
		// All items were already processed. There may actually be additional items if we retrieve
		// another page (since items keep being added in a running system) but we want to process
		// only the items that were available when the iterator was created.
		return nil, ErrNotFound
	}

	if it.currentIndex >= len(it.currentItems) {
		err := it.getNextPage()
		if err != nil {
			return nil, err
		}
	}

	item := it.currentItems[it.currentIndex]

	it.currentIndex++
	it.numProcessed++

	return item, nil
}

func (it *activityIterator) TotalItems() int {
	return it.totalItems
}

func (it *activityIterator) getNextPage() error {
	if it.nextPage == nil {
		return ErrNotFound
	}

	logger.Debug("Retrieving next page of activities", logfields.WithNextIRI(it.nextPage))

	respBytes, err := it.get(it.ctx, it.nextPage)
	if err != nil {
		return fmt.Errorf("get activities from %s: %w", it.nextPage, err)
	}

	logger.Debug("Got next page of activities", logfields.WithRequestURL(it.nextPage), log.WithResponse(respBytes))

	page, err := unmarshalCollectionPage(respBytes)
	if err != nil {
		return err
	}

	var activities []*vocab.ActivityType

	for _, item := range page.items {
		if item.Activity() != nil {
			logger.Debug("Adding activity to the recipient list",
				logfields.WithActivityID(item.Activity().ID()), logfields.WithActivityType(item.Activity().Type().String()))

			activities = it.appendActivity(activities, item.Activity())
		} else {
			logger.Warn("expecting activity item for collection but got a different type",
				logfields.WithType(item.Type().String()))
		}
	}

	it.currentIndex = 0
	it.currentItems = activities
	it.currentPage = page.current
	it.nextPage = it.getNext(page.next, page.prev)

	if len(it.currentItems) == 0 {
		return ErrNotFound
	}

	return nil
}

func newForwardActivityIterator(ctx context.Context, items []*vocab.ActivityType, currentPage, nextPage *url.URL,
	totalItems int, retrieve getFunc) *activityIterator {
	return newActivityIterator(ctx, items, currentPage, nextPage, totalItems, retrieve,
		func(next, _ *url.URL) *url.URL {
			return next
		},
		func(activities []*vocab.ActivityType, activity *vocab.ActivityType) []*vocab.ActivityType {
			return append(activities, activity)
		},
	)
}

func newReverseActivityIterator(ctx context.Context, items []*vocab.ActivityType, currentPage, nextPage *url.URL,
	totalItems int, retrieve getFunc) *activityIterator {
	return newActivityIterator(ctx, reverseSort(items), currentPage, nextPage, totalItems, retrieve,
		func(_, prev *url.URL) *url.URL {
			return prev
		},
		func(activities []*vocab.ActivityType, activity *vocab.ActivityType) []*vocab.ActivityType {
			// Prepend the activity since we're iterating in reverseSort order.
			return append([]*vocab.ActivityType{activity}, activities...)
		},
	)
}

func unmarshalCollection(respBytes []byte) (items []*vocab.ObjectProperty, firstPage, lastPage *url.URL,
	totalCount int, err error) {
	obj := &vocab.ObjectType{}

	if err := json.Unmarshal(respBytes, &obj); err != nil {
		return nil, nil, nil, 0, err
	}

	switch {
	case obj.Type().Is(vocab.TypeService):
		actor := &vocab.ActorType{}
		if err := json.Unmarshal(respBytes, actor); err != nil {
			return nil, nil, nil, 0, fmt.Errorf("invalid service in response: %w", err)
		}

		return []*vocab.ObjectProperty{vocab.NewObjectProperty(vocab.WithIRI(actor.ID().URL()))},
			nil, nil, 1, nil

	case obj.Type().Is(vocab.TypeCollection):
		coll := &vocab.CollectionType{}
		if err := json.Unmarshal(respBytes, coll); err != nil {
			return nil, nil, nil, 0, fmt.Errorf("invalid collection in response: %w", err)
		}

		return nil, coll.First(), coll.Last(), coll.TotalItems(), nil

	case obj.Type().Is(vocab.TypeOrderedCollection):
		coll := &vocab.OrderedCollectionType{}
		if err := json.Unmarshal(respBytes, coll); err != nil {
			return nil, nil, nil, 0,
				fmt.Errorf("invalid ordered collection in response: %w", err)
		}

		return nil, coll.First(), coll.Last(), coll.TotalItems(), nil

	default:
		return nil, nil, nil, 0,
			fmt.Errorf("expecting Service, Collection, OrderedCollection, CollectionPage, " +
				"or OrderedCollectionPage in response payload")
	}
}

type page struct {
	items               []*vocab.ObjectProperty
	current, next, prev *url.URL
	totalItems          int
}

func unmarshalCollectionPage(respBytes []byte) (*page, error) {
	obj := &vocab.ObjectType{}

	if err := json.Unmarshal(respBytes, &obj); err != nil {
		return nil, err
	}

	switch {
	case obj.Type().Is(vocab.TypeCollectionPage):
		coll := &vocab.CollectionPageType{}

		err := json.Unmarshal(respBytes, coll)
		if err != nil {
			return nil, fmt.Errorf("invalid collection page in response: %w", err)
		}

		return &page{
			items:      coll.Items(),
			current:    coll.ID().URL(),
			next:       coll.Next(),
			prev:       coll.Prev(),
			totalItems: coll.TotalItems(),
		}, nil

	case obj.Type().Is(vocab.TypeOrderedCollectionPage):
		coll := &vocab.OrderedCollectionPageType{}

		err := json.Unmarshal(respBytes, coll)
		if err != nil {
			return nil, fmt.Errorf("invalid ordered collection page in response: %w", err)
		}

		return &page{
			items:      coll.Items(),
			current:    coll.ID().URL(),
			next:       coll.Next(),
			prev:       coll.Prev(),
			totalItems: coll.TotalItems(),
		}, nil

	default:
		return nil, fmt.Errorf("expecting CollectionPage or OrderedCollectionPage in response payload")
	}
}

func reverseSort(items []*vocab.ActivityType) []*vocab.ActivityType {
	sort.SliceStable(items,
		func(i, j int) bool {
			return i > j //nolint:gocritic
		},
	)

	return items
}
