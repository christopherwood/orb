/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package observer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/hyperledger/aries-framework-go/component/storageutil/mem"
	"github.com/hyperledger/aries-framework-go/pkg/doc/signature/verifier"
	"github.com/hyperledger/aries-framework-go/pkg/doc/util"
	"github.com/hyperledger/aries-framework-go/pkg/doc/verifiable"
	"github.com/stretchr/testify/require"
	"github.com/trustbloc/sidetree-core-go/pkg/mocks"

	apclientmocks "github.com/trustbloc/orb/pkg/activitypub/client/mocks"
	"github.com/trustbloc/orb/pkg/activitypub/client/transport"
	apmocks "github.com/trustbloc/orb/pkg/activitypub/service/mocks"
	"github.com/trustbloc/orb/pkg/activitypub/vocab"
	"github.com/trustbloc/orb/pkg/anchor/anchorlinkset"
	"github.com/trustbloc/orb/pkg/anchor/anchorlinkset/generator"
	"github.com/trustbloc/orb/pkg/anchor/builder"
	"github.com/trustbloc/orb/pkg/anchor/graph"
	anchorinfo "github.com/trustbloc/orb/pkg/anchor/info"
	"github.com/trustbloc/orb/pkg/anchor/subject"
	casresolver "github.com/trustbloc/orb/pkg/cas/resolver"
	"github.com/trustbloc/orb/pkg/datauri"
	"github.com/trustbloc/orb/pkg/didanchor/memdidanchor"
	orberrors "github.com/trustbloc/orb/pkg/errors"
	"github.com/trustbloc/orb/pkg/internal/testutil"
	"github.com/trustbloc/orb/pkg/linkset"
	orbmocks "github.com/trustbloc/orb/pkg/mocks"
	obsmocks "github.com/trustbloc/orb/pkg/observer/mocks"
	protomocks "github.com/trustbloc/orb/pkg/protocolversion/mocks"
	"github.com/trustbloc/orb/pkg/pubsub/mempubsub"
	"github.com/trustbloc/orb/pkg/pubsub/spi"
	"github.com/trustbloc/orb/pkg/store/cas"
	webfingerclient "github.com/trustbloc/orb/pkg/webfinger/client"
)

//go:generate counterfeiter -o ../mocks/anchorgraph.gen.go --fake-name AnchorGraph . AnchorGraph
//go:generate counterfeiter -o ../mocks/anchorlinkstore.gen.go --fake-name AnchorLinkStore . linkStore
//go:generate counterfeiter -o ./mocks/monitoring.gen.go --fake-name MonitoringService . monitoringSvc

type linkStore interface { //nolint:unused
	PutLinks(links []*url.URL) error
	GetLinks(anchorHash string) ([]*url.URL, error)
	DeleteLinks(links []*url.URL) error
	DeletePendingLinks(links []*url.URL) error
}

const casLink = "https://domain.com/cas"

var serviceIRI = testutil.MustParseURL("https://domain1.com/services/orb")

func TestNew(t *testing.T) {
	errExpected := errors.New("injected pub-sub error")

	ps := &orbmocks.PubSub{}
	ps.SubscribeWithOptsReturns(nil, errExpected)

	providers := &Providers{
		DidAnchors: memdidanchor.New(),
		PubSub:     ps,
		Metrics:    &orbmocks.MetricsProvider{},
	}

	o, err := New(serviceIRI, providers)
	require.Error(t, err)
	require.Contains(t, err.Error(), errExpected.Error())
	require.Nil(t, o)
}

//nolint:maintidx
func TestStartObserver(t *testing.T) {
	const (
		namespace1 = "did:orb"
		namespace2 = "did:test"
	)

	t.Run("test channel close", func(t *testing.T) {
		providers := &Providers{
			DidAnchors: memdidanchor.New(),
			PubSub:     mempubsub.New(mempubsub.DefaultConfig()),
			Metrics:    &orbmocks.MetricsProvider{},
			Pkf:        pubKeyFetcherFnc,
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)
		require.NotNil(t, o.Publisher())

		o.Start()
		defer o.Stop()

		time.Sleep(200 * time.Millisecond)
	})

	t.Run("success - process batch", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		casClient, err := cas.New(mem.NewProvider(), casLink, nil, &orbmocks.MetricsProvider{}, 0)

		require.NoError(t, err)

		graphProviders := &graph.Providers{
			CasWriter: casClient,
			CasResolver: casresolver.New(casClient, nil,
				casresolver.NewWebCASResolver(
					transport.New(&http.Client{}, testutil.MustParseURL("https://example.com/keys/public-key"),
						transport.DefaultSigner(), transport.DefaultSigner(), &apclientmocks.AuthTokenMgr{}),
					webfingerclient.New(), "https"), &orbmocks.MetricsProvider{}),
			DocLoader: testutil.GetLoader(t),
		}

		anchorGraph := graph.New(graphProviders)

		prevAnchors := []*subject.SuffixAnchor{
			{Suffix: "did1"},
		}

		payload1 := subject.Payload{
			Namespace:       namespace1,
			Version:         0,
			CoreIndex:       "hl:uEiBGozN2uP1HBNNZtL-oeg2ifE0NuKY8Bg3miVMJtVZvYQ",
			PreviousAnchors: prevAnchors,
		}

		cid, err := anchorGraph.Add(newMockAnchorLinkset(t, &payload1))
		require.NoError(t, err)
		anchor1 := &anchorinfo.AnchorInfo{
			Hashlink:      cid,
			LocalHashlink: cid,
			AttributedTo:  "https://example.com/services/orb",
		}

		prevAnchors = []*subject.SuffixAnchor{
			{Suffix: "did2"},
		}

		payload2 := subject.Payload{
			Namespace:       namespace2,
			Version:         1,
			CoreIndex:       "hl:uEiC3Q4SF3bP-qb0i9MIz_k_n-rKi-BhSgcOk8qoKVcJqrg",
			PreviousAnchors: prevAnchors,
		}

		cid, err = anchorGraph.Add(newMockAnchorLinkset(t, &payload2))
		require.NoError(t, err)
		anchor2 := &anchorinfo.AnchorInfo{Hashlink: cid}

		payload3 := subject.Payload{
			Namespace:       namespace1,
			Version:         0,
			CoreIndex:       "hl:uEiCWKM6q1fGqlpW4HjpXYP5KbM8bLRQv_wZkDwyV_rp_JQ",
			PreviousAnchors: prevAnchors,
		}

		cid, err = anchorGraph.Add(newMockAnchorLinkset(t, &payload3))
		require.NoError(t, err)

		anchor3 := &anchorinfo.AnchorInfo{
			Hashlink:      cid,
			LocalHashlink: cid,
			AttributedTo:  "https://orb.domain2.com/services/orb",
		}

		casResolver := &protomocks.CASResolver{}
		casResolver.ResolveReturns([]byte(anchorEvent), "", nil)

		providers := &Providers{
			ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
			AnchorGraph:            anchorGraph,
			DidAnchors:             memdidanchor.New(),
			PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
			Metrics:                &orbmocks.MetricsProvider{},
			Outbox:                 func() Outbox { return apmocks.NewOutbox() },
			HostMetaLinkResolver:   &apmocks.WebFingerResolver{},
			CASResolver:            casResolver,
			DocLoader:              testutil.GetLoader(t),
			Pkf:                    pubKeyFetcherFnc,
			AnchorLinkStore:        &orbmocks.AnchorLinkStore{},
			MonitoringSvc:          &obsmocks.MonitoringService{},
			AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		o, err := New(serviceIRI, providers,
			WithDiscoveryDomain("webcas:shared.domain.com"),
			WithProofMonitoringExpiryPeriod(20*time.Second),
			WithSubscriberPoolSize(3))
		require.NotNil(t, o)
		require.NoError(t, err)

		o.Start()
		defer o.Stop()

		require.NoError(t, o.pubSub.PublishAnchor(context.Background(), anchor1))
		require.NoError(t, o.pubSub.PublishAnchor(context.Background(), anchor2))
		require.NoError(t, o.pubSub.PublishAnchor(context.Background(), anchor3))

		time.Sleep(200 * time.Millisecond)

		require.Equal(t, 2, tp.ProcessCallCount())
	})

	t.Run("success - process did (multiple, just create)", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		casClient, err := cas.New(mem.NewProvider(), casLink, nil, &orbmocks.MetricsProvider{}, 0)

		require.NoError(t, err)

		graphProviders := &graph.Providers{
			CasWriter: casClient,
			CasResolver: casresolver.New(casClient, nil,
				casresolver.NewWebCASResolver(
					transport.New(&http.Client{}, testutil.MustParseURL("https://example.com/keys/public-key"),
						transport.DefaultSigner(), transport.DefaultSigner(), &apclientmocks.AuthTokenMgr{}),
					webfingerclient.New(), "https"), &orbmocks.MetricsProvider{}),
			DocLoader:            testutil.GetLoader(t),
			AnchorLinksetBuilder: anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		anchorGraph := graph.New(graphProviders)

		did1 := "xyz"
		did2 := "abc"

		previousAnchors := []*subject.SuffixAnchor{
			{Suffix: did1},
			{Suffix: did2},
		}

		payload1 := subject.Payload{
			Namespace:       namespace1,
			Version:         0,
			CoreIndex:       "hl:uEiC_17B7wGGQ61SZi2QDQMpQcB-cqLZz1mdBOPcT3cAZBA",
			PreviousAnchors: previousAnchors,
		}

		cid, err := anchorGraph.Add(newMockAnchorLinkset(t, &payload1))
		require.NoError(t, err)

		providers := &Providers{
			ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
			AnchorGraph:            anchorGraph,
			DidAnchors:             memdidanchor.New(),
			PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
			Metrics:                &orbmocks.MetricsProvider{},
			Pkf:                    pubKeyFetcherFnc,
			DocLoader:              testutil.GetLoader(t),
			AnchorLinkStore:        &orbmocks.AnchorLinkStore{},
			AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		o.Start()
		defer o.Stop()

		require.NoError(t, o.pubSub.PublishDID(context.Background(), cid+":"+did1))
		require.NoError(t, o.pubSub.PublishDID(context.Background(), cid+":"+did2))

		time.Sleep(200 * time.Millisecond)

		require.Equal(t, 2, tp.ProcessCallCount())
	})

	t.Run("success - process did with previous anchors", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		casClient, err := cas.New(mem.NewProvider(), casLink, nil, &orbmocks.MetricsProvider{}, 0)

		require.NoError(t, err)

		graphProviders := &graph.Providers{
			CasWriter: casClient,
			CasResolver: casresolver.New(casClient, nil,
				casresolver.NewWebCASResolver(
					transport.New(&http.Client{}, testutil.MustParseURL("https://example.com/keys/public-key"),
						transport.DefaultSigner(), transport.DefaultSigner(), &apclientmocks.AuthTokenMgr{}),
					webfingerclient.New(), "https"), &orbmocks.MetricsProvider{}),
			DocLoader:            testutil.GetLoader(t),
			AnchorLinksetBuilder: anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		anchorGraph := graph.New(graphProviders)

		did1 := "jkh"

		previousAnchors := []*subject.SuffixAnchor{
			{Suffix: did1},
		}

		payload1 := subject.Payload{
			Namespace:       namespace1,
			Version:         0,
			CoreIndex:       "hl:uEiC_17B7wGGQ61SZi2QDQMpQcB-cqLZz1mdBOPcT3cAZBA",
			PreviousAnchors: previousAnchors,
		}

		cid, err := anchorGraph.Add(newMockAnchorLinkset(t, &payload1))
		require.NoError(t, err)

		previousAnchors[0].Anchor = cid

		payload2 := subject.Payload{
			Namespace:       namespace1,
			Version:         0,
			CoreIndex:       "hl:uEiC_17B7wGGQ61SZi2QDQMpQcB-cqLZz1mdBOPcT3cAZBA",
			PreviousAnchors: previousAnchors,
		}

		cid, err = anchorGraph.Add(newMockAnchorLinkset(t, &payload2))
		require.NoError(t, err)

		providers := &Providers{
			ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
			AnchorGraph:            anchorGraph,
			DidAnchors:             memdidanchor.New(),
			PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
			Metrics:                &orbmocks.MetricsProvider{},
			DocLoader:              testutil.GetLoader(t),
			Pkf:                    pubKeyFetcherFnc,
			AnchorLinkStore:        &orbmocks.AnchorLinkStore{},
			AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		o.Start()
		defer o.Stop()

		require.NoError(t, o.pubSub.PublishDID(context.Background(), cid+":"+did1))
		time.Sleep(200 * time.Millisecond)

		require.Equal(t, 2, tp.ProcessCallCount())
	})

	t.Run("success - did and anchor", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		casClient, err := cas.New(mem.NewProvider(), casLink, nil, &orbmocks.MetricsProvider{}, 0)

		require.NoError(t, err)

		graphProviders := &graph.Providers{
			CasWriter: casClient,
			CasResolver: casresolver.New(casClient, nil,
				casresolver.NewWebCASResolver(
					transport.New(&http.Client{}, testutil.MustParseURL("https://example.com/keys/public-key"),
						transport.DefaultSigner(), transport.DefaultSigner(), &apclientmocks.AuthTokenMgr{}),
					webfingerclient.New(), "https"), &orbmocks.MetricsProvider{}),
			DocLoader:            testutil.GetLoader(t),
			AnchorLinksetBuilder: anchorlinkset.NewBuilder(generator.NewRegistry()),
		}
		anchorGraph := graph.New(graphProviders)

		did := "123"

		previousDIDAnchors := []*subject.SuffixAnchor{
			{Suffix: did},
		}

		payload1 := subject.Payload{
			Namespace: namespace1,
			Version:   0, CoreIndex: "hl:uEiC_17B7wGGQ61SZi2QDQMpQcB-cqLZz1mdBOPcT3cAZBA",
			PreviousAnchors: previousDIDAnchors,
		}

		cid, err := anchorGraph.Add(newMockAnchorLinkset(t, &payload1))
		require.NoError(t, err)

		anchor := &anchorinfo.AnchorInfo{Hashlink: cid}

		providers := &Providers{
			ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
			AnchorGraph:            anchorGraph,
			DidAnchors:             memdidanchor.New(),
			PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
			Metrics:                &orbmocks.MetricsProvider{},
			DocLoader:              testutil.GetLoader(t),
			Pkf:                    pubKeyFetcherFnc,
			AnchorLinkStore:        &orbmocks.AnchorLinkStore{},
			AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		o.Start()
		defer o.Stop()

		require.NoError(t, o.pubSub.PublishAnchor(context.Background(), anchor))
		require.NoError(t, o.pubSub.PublishDID(context.Background(), cid+":"+did))
		time.Sleep(200 * time.Millisecond)

		require.Equal(t, 2, tp.ProcessCallCount())
	})

	t.Run("error - transaction processor error", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		casClient, err := cas.New(mem.NewProvider(), casLink, nil, &orbmocks.MetricsProvider{}, 0)

		require.NoError(t, err)

		graphProviders := &graph.Providers{
			CasWriter: casClient,
			CasResolver: casresolver.New(casClient, nil,
				casresolver.NewWebCASResolver(
					transport.New(&http.Client{}, testutil.MustParseURL("https://example.com/keys/public-key"),
						transport.DefaultSigner(), transport.DefaultSigner(), &apclientmocks.AuthTokenMgr{}),
					webfingerclient.New(), "https"), &orbmocks.MetricsProvider{}),
			DocLoader:            testutil.GetLoader(t),
			AnchorLinksetBuilder: anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		anchorGraph := graph.New(graphProviders)

		did1 := "123"
		did2 := "abc"

		previousAnchors := []*subject.SuffixAnchor{
			{Suffix: did1},
			{Suffix: did2},
		}

		payload1 := subject.Payload{
			Namespace:       namespace1,
			Version:         0,
			CoreIndex:       "hl:uEiC_17B7wGGQ61SZi2QDQMpQcB-cqLZz1mdBOPcT3cAZBA",
			PreviousAnchors: previousAnchors,
		}

		cid, err := anchorGraph.Add(newMockAnchorLinkset(t, &payload1))
		require.NoError(t, err)

		providers := &Providers{
			ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
			AnchorGraph:            anchorGraph,
			DidAnchors:             memdidanchor.New(),
			PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
			Metrics:                &orbmocks.MetricsProvider{},
			DocLoader:              testutil.GetLoader(t),
			AnchorLinkStore:        &orbmocks.AnchorLinkStore{},
			AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		o.Start()
		defer o.Stop()

		require.NoError(t, o.pubSub.PublishDID(context.Background(), cid+":"+did1))
		require.NoError(t, o.pubSub.PublishDID(context.Background(), cid+":"+did2))

		time.Sleep(200 * time.Millisecond)

		require.Equal(t, 2, tp.ProcessCallCount())
	})

	t.Run("error - update did anchors error", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		casClient, err := cas.New(mem.NewProvider(), casLink, nil, &orbmocks.MetricsProvider{}, 0)

		require.NoError(t, err)

		graphProviders := &graph.Providers{
			CasWriter: casClient,
			CasResolver: casresolver.New(casClient, nil,
				casresolver.NewWebCASResolver(
					transport.New(&http.Client{}, testutil.MustParseURL("https://example.com/keys/public-key"),
						transport.DefaultSigner(), transport.DefaultSigner(), &apclientmocks.AuthTokenMgr{}),
					webfingerclient.New(), "https"), &orbmocks.MetricsProvider{}),
			DocLoader:            testutil.GetLoader(t),
			AnchorLinksetBuilder: anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		anchorGraph := graph.New(graphProviders)

		prevAnchors := []*subject.SuffixAnchor{
			{Suffix: "suffix"},
		}

		payload1 := subject.Payload{
			Namespace:       namespace1,
			Version:         0,
			CoreIndex:       "hl:uEiBqkaTRFZScQsXTw8IDBSpVxiKGqjJCDUcgiwpcd2frLw",
			PreviousAnchors: prevAnchors,
		}

		cid, err := anchorGraph.Add(newMockAnchorLinkset(t, &payload1))
		require.NoError(t, err)
		anchor1 := &anchorinfo.AnchorInfo{Hashlink: cid}

		payload2 := subject.Payload{
			Namespace:       namespace2,
			Version:         1,
			CoreIndex:       "hl:uEiC3Q4SF3bP-qb0i9MIz_k_n-rKi-BhSgcOk8qoKVcJqrg",
			PreviousAnchors: prevAnchors,
		}

		cid, err = anchorGraph.Add(newMockAnchorLinkset(t, &payload2))
		require.NoError(t, err)
		anchor2 := &anchorinfo.AnchorInfo{Hashlink: cid}

		providers := &Providers{
			ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
			AnchorGraph:            anchorGraph,
			DidAnchors:             &mockDidAnchor{Err: fmt.Errorf("did anchor error")},
			PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
			Metrics:                &orbmocks.MetricsProvider{},
			DocLoader:              testutil.GetLoader(t),
			Pkf:                    pubKeyFetcherFnc,
			AnchorLinkStore:        &orbmocks.AnchorLinkStore{},
			AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		o.Start()
		defer o.Stop()

		require.NoError(t, o.pubSub.PublishAnchor(context.Background(), anchor1))
		require.NoError(t, o.pubSub.PublishAnchor(context.Background(), anchor2))

		time.Sleep(200 * time.Millisecond)

		require.Equal(t, 1, tp.ProcessCallCount())
	})

	t.Run("error - cid not found", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		casClient, err := cas.New(mem.NewProvider(), casLink, nil, &orbmocks.MetricsProvider{}, 0)
		require.NoError(t, err)

		graphProviders := &graph.Providers{
			CasWriter: casClient,
			CasResolver: casresolver.New(casClient, nil,
				casresolver.NewWebCASResolver(
					transport.New(&http.Client{}, testutil.MustParseURL("https://example.com/keys/public-key"),
						transport.DefaultSigner(), transport.DefaultSigner(), &apclientmocks.AuthTokenMgr{}),
					webfingerclient.New(), "https"), &orbmocks.MetricsProvider{}),
			DocLoader:            testutil.GetLoader(t),
			AnchorLinksetBuilder: anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		anchorGraph := graph.New(graphProviders)

		providers := &Providers{
			ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
			AnchorGraph:            anchorGraph,
			DidAnchors:             memdidanchor.New(),
			PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
			Metrics:                &orbmocks.MetricsProvider{},
			DocLoader:              testutil.GetLoader(t),
			Pkf:                    pubKeyFetcherFnc,
			AnchorLinkStore:        &orbmocks.AnchorLinkStore{},
			AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		o.Start()
		defer o.Stop()

		require.NoError(t, o.pubSub.PublishDID(context.Background(), "cid:did"))
		time.Sleep(200 * time.Millisecond)

		require.Equal(t, 0, tp.ProcessCallCount())
	})

	t.Run("error - invalid did format", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		providers := &Providers{
			ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
			DidAnchors:             memdidanchor.New(),
			PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
			Metrics:                &orbmocks.MetricsProvider{},
			DocLoader:              testutil.GetLoader(t),
			Pkf:                    pubKeyFetcherFnc,
			AnchorLinkStore:        &orbmocks.AnchorLinkStore{},
			AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		o.Start()
		defer o.Stop()

		require.NoError(t, o.pubSub.PublishDID(context.Background(), "no-cid"))
		time.Sleep(200 * time.Millisecond)

		require.Equal(t, 0, tp.ProcessCallCount())
	})

	t.Run("PublishDID persistent error in process anchor -> ignore", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		anchorGraph := &orbmocks.AnchorGraph{}
		anchorGraph.GetDidAnchorsReturns([]graph.Anchor{{
			Info: &linkset.Link{},
		}}, nil)

		providers := &Providers{
			ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
			AnchorGraph:            anchorGraph,
			DidAnchors:             memdidanchor.New(),
			PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
			Metrics:                &orbmocks.MetricsProvider{},
			DocLoader:              testutil.GetLoader(t),
			Pkf:                    pubKeyFetcherFnc,
			AnchorLinkStore:        &orbmocks.AnchorLinkStore{},
			AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		o.Start()
		defer o.Stop()

		require.NoError(t, o.pubSub.PublishDID(context.Background(), "cid:xyz"))
		time.Sleep(200 * time.Millisecond)

		require.Empty(t, tp.ProcessCallCount())
	})

	t.Run("PublishDID transient error in process anchor -> error", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}
		tp.ProcessReturns(0, orberrors.NewTransient(errors.New("injected processing error")))

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		casClient, err := cas.New(mem.NewProvider(), casLink, nil, &orbmocks.MetricsProvider{}, 0)
		require.NoError(t, err)

		graphProviders := &graph.Providers{
			CasWriter: casClient,
			CasResolver: casresolver.New(casClient, nil,
				casresolver.NewWebCASResolver(
					transport.New(&http.Client{}, testutil.MustParseURL("https://example.com/keys/public-key"),
						transport.DefaultSigner(), transport.DefaultSigner(), &apclientmocks.AuthTokenMgr{}),
					webfingerclient.New(), "https"), &orbmocks.MetricsProvider{}),
			DocLoader:            testutil.GetLoader(t),
			AnchorLinksetBuilder: anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		anchorGraph := graph.New(graphProviders)

		did1 := "xyz"

		previousAnchors := []*subject.SuffixAnchor{
			{Suffix: did1},
		}

		payload1 := subject.Payload{
			Namespace:       namespace1,
			Version:         0,
			CoreIndex:       "hl:uEiC_17B7wGGQ61SZi2QDQMpQcB-cqLZz1mdBOPcT3cAZBA",
			PreviousAnchors: previousAnchors,
		}

		cid, err := anchorGraph.Add(newMockAnchorLinkset(t, &payload1))
		require.NoError(t, err)

		pubSub := apmocks.NewPubSub()
		defer pubSub.Stop()

		undeliverableChan, err := pubSub.Subscribe(context.Background(), spi.UndeliverableTopic)
		require.NoError(t, err)

		providers := &Providers{
			ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
			AnchorGraph:            anchorGraph,
			DidAnchors:             memdidanchor.New(),
			PubSub:                 pubSub,
			Metrics:                &orbmocks.MetricsProvider{},
			DocLoader:              testutil.GetLoader(t),
			Pkf:                    pubKeyFetcherFnc,
			AnchorLinkStore:        &orbmocks.AnchorLinkStore{},
			AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		o.Start()
		defer o.Stop()

		require.NoError(t, o.pubSub.PublishDID(context.Background(), cid+":"+did1))

		select {
		case msg := <-undeliverableChan:
			t.Logf("Got undeliverable message: %s", msg.UUID)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Expecting undeliverable message")
		}
	})

	t.Run("success - process duplicate operations", func(t *testing.T) {
		tp := &mocks.TxnProcessor{}
		tp.ProcessReturns(0, nil)

		pc := mocks.NewMockProtocolClient()
		pc.Versions[0].TransactionProcessorReturns(tp)
		pc.Versions[0].ProtocolReturns(pc.Protocol)

		casClient, err := cas.New(mem.NewProvider(), casLink, nil, &orbmocks.MetricsProvider{}, 0)

		require.NoError(t, err)

		graphProviders := &graph.Providers{
			CasWriter: casClient,
			CasResolver: casresolver.New(casClient, nil,
				casresolver.NewWebCASResolver(
					transport.New(&http.Client{}, testutil.MustParseURL("https://example.com/keys/public-key"),
						transport.DefaultSigner(), transport.DefaultSigner(), &apclientmocks.AuthTokenMgr{}),
					webfingerclient.New(), "https"), &orbmocks.MetricsProvider{}),
			DocLoader:            testutil.GetLoader(t),
			AnchorLinksetBuilder: anchorlinkset.NewBuilder(generator.NewRegistry()),
		}

		anchorGraph := graph.New(graphProviders)

		prevAnchors := []*subject.SuffixAnchor{
			{Suffix: "did1"},
		}

		payload1 := subject.Payload{
			Namespace:       namespace1,
			Version:         0,
			CoreIndex:       "hl:uEiBqkaTRFZScQsXTw8IDBSpVxiKGqjJCDUcgiwpcd2frLw",
			PreviousAnchors: prevAnchors,
		}

		cid, err := anchorGraph.Add(newMockAnchorLinkset(t, &payload1))
		require.NoError(t, err)
		anchor1 := &anchorinfo.AnchorInfo{
			Hashlink:      cid,
			LocalHashlink: cid,
			AttributedTo:  "https://example.com/services/orb",
		}

		casResolver := &protomocks.CASResolver{}
		casResolver.ResolveReturns([]byte(anchorEvent), "", nil)

		t.Run("no operations", func(t *testing.T) {
			anchorLinkStore := &orbmocks.AnchorLinkStore{}
			anchorLinkStore.GetLinksReturns([]*url.URL{testutil.MustParseURL(anchor1.Hashlink)}, nil)

			providers := &Providers{
				ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
				AnchorGraph:            anchorGraph,
				DidAnchors:             memdidanchor.New(),
				PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
				Metrics:                &orbmocks.MetricsProvider{},
				Outbox:                 func() Outbox { return apmocks.NewOutbox() },
				HostMetaLinkResolver:   &apmocks.WebFingerResolver{},
				CASResolver:            casResolver,
				DocLoader:              testutil.GetLoader(t),
				Pkf:                    pubKeyFetcherFnc,
				AnchorLinkStore:        anchorLinkStore,
				AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
			}

			o, err := New(serviceIRI, providers, WithDiscoveryDomain("webcas:shared.domain.com"))
			require.NotNil(t, o)
			require.NoError(t, err)

			o.Start()
			defer o.Stop()

			require.NoError(t, o.pubSub.PublishAnchor(context.Background(), anchor1))

			time.Sleep(200 * time.Millisecond)

			require.Equal(t, 1, tp.ProcessCallCount())
		})

		t.Run("GetLinks error", func(t *testing.T) {
			anchorLinkStore := &orbmocks.AnchorLinkStore{}
			anchorLinkStore.GetLinksReturns(nil, errors.New("injected GetLinks error"))

			providers := &Providers{
				ProtocolClientProvider: mocks.NewMockProtocolClientProvider().WithProtocolClient(namespace1, pc),
				AnchorGraph:            anchorGraph,
				DidAnchors:             memdidanchor.New(),
				PubSub:                 mempubsub.New(mempubsub.DefaultConfig()),
				Metrics:                &orbmocks.MetricsProvider{},
				Outbox:                 func() Outbox { return apmocks.NewOutbox() },
				HostMetaLinkResolver:   &apmocks.WebFingerResolver{},
				CASResolver:            casResolver,
				DocLoader:              testutil.GetLoader(t),
				Pkf:                    pubKeyFetcherFnc,
				AnchorLinkStore:        anchorLinkStore,
				AnchorLinksetBuilder:   anchorlinkset.NewBuilder(generator.NewRegistry()),
			}

			o, err := New(serviceIRI, providers, WithDiscoveryDomain("webcas:shared.domain.com"))
			require.NotNil(t, o)
			require.NoError(t, err)

			o.Start()
			defer o.Stop()

			require.NoError(t, o.pubSub.PublishAnchor(context.Background(), anchor1))

			time.Sleep(200 * time.Millisecond)
		})
	})
}

func TestResolveActorFromHashlink(t *testing.T) {
	const hl = "hl:uEiBdcSP14brpoA76draKLGbh4cfxhrRfTWq7Ay3A3RVJyw:uoQ-BeEtodHRwczovL29yYi5kb21haW4yLmNvbS9jYXMvdUVpQmRjU1AxNGJycG9BNzZkcmFLTEdiaDRjZnhoclJmVFdxN0F5M0EzUlZKeXc"

	casResolver := &protomocks.CASResolver{}
	wfResolver := &apmocks.WebFingerResolver{}

	providers := &Providers{
		PubSub:               mempubsub.New(mempubsub.DefaultConfig()),
		HostMetaLinkResolver: wfResolver,
		CASResolver:          casResolver,
		DocLoader:            testutil.GetLoader(t),
		Pkf:                  pubKeyFetcherFnc,
		AnchorLinkStore:      &orbmocks.AnchorLinkStore{},
	}

	o, e := New(serviceIRI, providers)
	require.NotNil(t, o)
	require.NoError(t, e)

	t.Run("Success", func(t *testing.T) {
		casResolver.ResolveReturns([]byte(anchorEvent), "", nil)

		actor, err := o.resolveActorFromHashlink(hl)
		require.NoError(t, err)
		require.Equal(t, "did:web:orb.domain2.com:services:orb", actor)
	})

	t.Run("CAS resolve error", func(t *testing.T) {
		errExpected := errors.New("injected resolve error")

		casResolver.ResolveReturns(nil, "", errExpected)

		_, err := o.resolveActorFromHashlink(hl)
		require.Error(t, err)
		require.Contains(t, err.Error(), errExpected.Error())
	})

	t.Run("Parse VC error", func(t *testing.T) {
		casResolver.ResolveReturns([]byte(anchorEventInvalid), "", nil)

		_, err := o.resolveActorFromHashlink(hl)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unexpected end of JSON input")
	})
}

func TestSetupProofMonitoring(t *testing.T) {
	vc, err := verifiable.ParseCredential([]byte(testVC),
		verifiable.WithDisabledProofCheck(),
		verifiable.WithJSONLDDocumentLoader(testutil.GetLoader(t)),
		verifiable.WithStrictValidation(),
	)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		providers := &Providers{
			PubSub:        mempubsub.New(mempubsub.DefaultConfig()),
			MonitoringSvc: &obsmocks.MonitoringService{},
		}

		o, e := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, e)

		o.setupProofMonitoring(vc)
	})

	t.Run("success - duplicate same proof(ignored)", func(t *testing.T) {
		providers := &Providers{
			PubSub:        mempubsub.New(mempubsub.DefaultConfig()),
			MonitoringSvc: &obsmocks.MonitoringService{},
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		vc, err = verifiable.ParseCredential([]byte(testVCDuplicateProof),
			verifiable.WithDisabledProofCheck(),
			verifiable.WithJSONLDDocumentLoader(testutil.GetLoader(t)),
			verifiable.WithStrictValidation(),
		)
		require.NoError(t, err)

		o.setupProofMonitoring(vc)
	})

	t.Run("success - monitoring service error (ignored)", func(t *testing.T) {
		svc := &obsmocks.MonitoringService{}

		svc.WatchReturns(fmt.Errorf("monitoring service error"))

		providers := &Providers{
			PubSub:        mempubsub.New(mempubsub.DefaultConfig()),
			MonitoringSvc: svc,
		}

		o, e := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, e)

		o.setupProofMonitoring(vc)
	})

	t.Run("success - parse proof created error (ignored)", func(t *testing.T) {
		providers := &Providers{
			PubSub:        mempubsub.New(mempubsub.DefaultConfig()),
			MonitoringSvc: &obsmocks.MonitoringService{},
		}

		o, err := New(serviceIRI, providers)
		require.NotNil(t, o)
		require.NoError(t, err)

		vc, err = verifiable.ParseCredential([]byte(testVCInvalidCreated),
			verifiable.WithDisabledProofCheck(),
			verifiable.WithJSONLDDocumentLoader(testutil.GetLoader(t)),
			verifiable.WithStrictValidation(),
		)
		require.NoError(t, err)

		o.setupProofMonitoring(vc)
	})
}

func newMockAnchorLinkset(t *testing.T, payload *subject.Payload) *linkset.Linkset {
	t.Helper()

	vc := &verifiable.Credential{
		Types:   []string{"VerifiableCredential", "AnchorCredential"},
		Context: []string{vocab.ContextCredentials, vocab.ContextActivityAnchors},
		Subject: &builder.CredentialSubject{
			HRef:    "hl:uEiBGozN2uP1HBNNZtL-oeg2ifE0NuKY8Bg3miVMJtVZvYQ",
			Type:    []string{"AnchorLink"},
			Profile: "https://w3id.org/orb#v0",
			Anchor:  "hl:uEiD7xzrz5lEKIq0ZZWh9ky0mNW6wxpGx_H2bxhg80c1IDA",
			Rel:     "linkset",
		},
		Issuer: verifiable.Issuer{
			ID: "https://orb.domain1.com",
		},
		Issued: &util.TimeWrapper{Time: time.Now()},
	}

	al, _, err := anchorlinkset.NewBuilder(
		generator.NewRegistry()).BuildAnchorLink(payload, datauri.MediaTypeDataURIGzipBase64,
		func(anchorHashlink, coreIndexHashlink string) (*verifiable.Credential, error) {
			return vc, nil
		},
	)
	require.NoError(t, err)

	return linkset.New(al)
}

var pubKeyFetcherFnc = func(issuerID, keyID string) (*verifier.PublicKey, error) {
	return nil, nil //nolint:nilnil
}

type mockDidAnchor struct {
	Err error
}

func (m *mockDidAnchor) PutBulk(_ []string, _ []bool, _ string) error {
	if m.Err != nil {
		return m.Err
	}

	return nil
}

const anchorEvent = `{
  "linkset": [
    {
      "anchor": "hl:uEiAr_xUtbeoALO4iKvN5eIWjqUmIO35wFEPTTzjOaSYgUA",
      "author": [
        {
          "href": "did:web:orb.domain2.com:services:orb"
        }
      ],
      "original": [
        {
          "href": "data:application/json,%7B%22linkset%22%3A%5B%7B%22anchor%22%3A%22hl%3AuEiDN_w5UTmfhZa-k9AwutAxw4qPSRPbxpwi9Ik9Tqh3wkg%22%2C%22author%22%3A%5B%7B%22href%22%3A%22did%3Aweb%3Aorb.domain2.com%3Aservices%3Aorb%22%7D%5D%2C%22item%22%3A%5B%7B%22href%22%3A%22did%3Aorb%3AuAAA%3AEiCHYLNrOLv5cYSVrTEtzSvOI3uQPukFzxsQMjfy8r25fA%22%7D%5D%2C%22profile%22%3A%5B%7B%22href%22%3A%22https%3A%2F%2Fw3id.org%2Forb%23v0%22%7D%5D%7D%5D%7D",
          "type": "application/linkset+json"
        }
      ],
      "profile": [
        {
          "href": "https://w3id.org/orb#v0"
        }
      ],
      "related": [
        {
          "href": "data:application/json,%7B%22linkset%22%3A%5B%7B%22anchor%22%3A%22hl%3AuEiAr_xUtbeoALO4iKvN5eIWjqUmIO35wFEPTTzjOaSYgUA%22%2C%22profile%22%3A%5B%7B%22href%22%3A%22https%3A%2F%2Fw3id.org%2Forb%23v0%22%7D%5D%2C%22via%22%3A%5B%7B%22href%22%3A%22hl%3AuEiDN_w5UTmfhZa-k9AwutAxw4qPSRPbxpwi9Ik9Tqh3wkg%3AuoQ-BeEtodHRwczovL29yYi5kb21haW4yLmNvbS9jYXMvdUVpRE5fdzVVVG1maFphLWs5QXd1dEF4dzRxUFNSUGJ4cHdpOUlrOVRxaDN3a2c%22%7D%5D%7D%5D%7D",
          "type": "application/linkset+json"
        }
      ],
      "replies": [
        {
          "href": "data:application/json,%7B%22%40context%22%3A%5B%22https%3A%2F%2Fwww.w3.org%2F2018%2Fcredentials%2Fv1%22%2C%22https%3A%2F%2Fw3id.org%2Factivityanchors%2Fv1%22%2C%22https%3A%2F%2Fw3id.org%2Fsecurity%2Fsuites%2Fjws-2020%2Fv1%22%2C%22https%3A%2F%2Fw3id.org%2Fsecurity%2Fsuites%2Fed25519-2020%2Fv1%22%5D%2C%22credentialSubject%22%3A%7B%22anchor%22%3A%22hl%3AuEiDN_w5UTmfhZa-k9AwutAxw4qPSRPbxpwi9Ik9Tqh3wkg%22%2C%22id%22%3A%22hl%3AuEiAr_xUtbeoALO4iKvN5eIWjqUmIO35wFEPTTzjOaSYgUA%22%2C%22profile%22%3A%22https%3A%2F%2Fw3id.org%2Forb%23v0%22%7D%2C%22id%22%3A%22https%3A%2F%2Forb.domain2.com%2Fvc%2F654bea3b-63a6-4ec7-a73f-0b63c083acc2%22%2C%22issuanceDate%22%3A%222022-08-24T13%3A12%3A21.317143941Z%22%2C%22issuer%22%3A%22https%3A%2F%2Forb.domain2.com%22%2C%22proof%22%3A%5B%7B%22created%22%3A%222022-08-24T13%3A12%3A21.318445635Z%22%2C%22domain%22%3A%22https%3A%2F%2Forb.domain2.com%22%2C%22proofPurpose%22%3A%22assertionMethod%22%2C%22proofValue%22%3A%22z2cWVavmjqJhjAYV1dYzaKLegq3y4wUgZgDvMJiHFdvyRhBgZQ5fqXyf3RKddS5GsBpsPg1GmEorALAqLkjPNXpWK%22%2C%22type%22%3A%22Ed25519Signature2020%22%2C%22verificationMethod%22%3A%22did%3Aweb%3Aorb.domain2.com%23ArGfVFUvJOYE79EO_yJOwgCCH56247AZ7KjK4NIUkug%22%7D%2C%7B%22created%22%3A%222022-08-24T13%3A12%3A21.514Z%22%2C%22domain%22%3A%22http%3A%2F%2Forb.vct%3A8077%2Fmaple2020%22%2C%22proofPurpose%22%3A%22assertionMethod%22%2C%22proofValue%22%3A%22z5TjC27rA4X9pRXXffGSB9wcPt8jiFXpP3RnJhMLa6EUKuaphnqY8kV1rtYLMVdthCqTs4GwkmmYGXm9V3NYx37X3%22%2C%22type%22%3A%22Ed25519Signature2020%22%2C%22verificationMethod%22%3A%22did%3Aweb%3Aorb.domain1.com%23hp8fYEk5G-PSNAFj4cGIRFtJmDCEEQ7MKUwAzocN3n0%22%7D%5D%2C%22type%22%3A%5B%22VerifiableCredential%22%2C%22AnchorCredential%22%5D%7D",
          "type": "application/ld+json"
        }
      ]
    }
  ]
}`

const anchorEventInvalid = `{
  "@context": [
`

const testVC = `{
  "@context": [
    "https://www.w3.org/2018/credentials/v1",
    "https://w3id.org/activityanchors/v1",
    "https://w3id.org/security/suites/jws-2020/v1",
    "https://w3id.org/security/suites/ed25519-2020/v1"
  ],
  "credentialSubject": {
    "anchor": "hl:uEiD7xzrz5lEKIq0ZZWh9ky0mNW6wxpGx_H2bxhg80c1IDA",
    "href": "hl:uEiBGozN2uP1HBNNZtL-oeg2ifE0NuKY8Bg3miVMJtVZvYQ",
    "type": ["AnchorLink"],
    "profile": "https://w3id.org/orb#v0",
    "rel": "linkset"
  },
  "id": "https://orb.domain1.com/vc/daad6147-8148-4917-969a-a8a529908281",
  "issuanceDate": "2022-08-22T16:39:55.014682121Z",
  "issuer": "https://orb.domain1.com",
  "proof": [
    {
      "created": "2022-08-22T16:39:55.022Z",
      "domain": "http://orb.vct:8077/maple2020",
      "jws": "eyJhbGciOiIiLCJiNjQiOmZhbHNlLCJjcml0IjpbImI2NCJdfQ..MEYCIQDPteXXbktG7ma_TNBVcz1bzSJtntbrYaNX9SvAgBj_5wIhAKRmM3ODG2Tvc4uZxUszZEfSMLeUTUzAUJXUdbQFFMyp",
      "proofPurpose": "assertionMethod",
      "type": "JsonWebSignature2020",
      "verificationMethod": "did:web:orb.domain1.com#alias/vc-sign"
    },
    {
      "created": "2022-08-22T16:39:55.088769427Z",
      "domain": "https://orb.domain2.com",
      "jws": "eyJhbGciOiIiLCJiNjQiOmZhbHNlLCJjcml0IjpbImI2NCJdfQ..MEUCIQDDYqGKZQbOqoQGUxLl4Rz3vpnjx4wX7Q0GzLZ6tVBXOwIgenRV_ishAAh5-mSb4qExKjeHk1hMDDWAFxbbny0JjcQ",
      "proofPurpose": "assertionMethod",
      "type": "JsonWebSignature2020",
      "verificationMethod": "did:web:orb.domain2.com#0y7nlEMkhY-903aO2Qhly8LAXxuHKjur20kF9k5Gy5w"
    }
  ],
  "type": [
    "VerifiableCredential",
    "AnchorCredential"
  ]
}`

const testVCDuplicateProof = `{
  "@context": [
    "https://www.w3.org/2018/credentials/v1",
    "https://w3id.org/activityanchors/v1",
    "https://w3id.org/security/suites/jws-2020/v1",
    "https://w3id.org/security/suites/ed25519-2020/v1"
  ],
  "credentialSubject": {
    "anchor": "hl:uEiD7xzrz5lEKIq0ZZWh9ky0mNW6wxpGx_H2bxhg80c1IDA",
    "href": "hl:uEiBGozN2uP1HBNNZtL-oeg2ifE0NuKY8Bg3miVMJtVZvYQ",
    "type": ["AnchorLink"],
    "profile": "https://w3id.org/orb#v0",
    "rel": "linkset"
  },
  "id": "https://orb.domain4.com/vc/61f38a18-e2dd-4f91-b376-9586ca189b25",
  "issuanceDate": "2022-07-15T19:17:55.2446168Z",
  "issuer": "https://orb.domain4.com",
  "proof": [
    {
      "created": "2022-07-15T19:17:55.246854Z",
      "domain": "https://orb.domain4.com",
      "proofPurpose": "assertionMethod",
      "proofValue": "MEYCIQDwuBrM_lgb6mVyXu6DzD2wa25WJA9AD9GsqWk1eeblSQIhANKgynJs6bP-W7mnryJ7TJryLdz9CHnMKtWqJ2XMmMBt",
      "type": "JsonWebSignature2020",
      "verificationMethod": "did:web:orb.domain4.com#alias/vc-sign"
    },
    {
      "created": "2022-07-15T19:17:55.246854Z",
      "domain": "https://orb.domain4.com",
      "proofPurpose": "assertionMethod",
      "proofValue": "MEYCIQDwuBrM_lgb6mVyXu6DzD2wa25WJA9AD9GsqWk1eeblSQIhANKgynJs6bP-W7mnryJ7TJryLdz9CHnMKtWqJ2XMmMBt",
      "type": "JsonWebSignature2020",
      "verificationMethod": "did:web:orb.domain4.com#alias/vc-sign"
    },
    {
      "created": "2022-07-15T19:17:59.5484674Z",
      "domain": "https://orb.domain2.com",
      "proofPurpose": "assertionMethod",
      "proofValue": "z3jbefjmhKeoirfakRAtD8UPySJjsY4Hb4t6fG5myAwMr1mV4ygGKXKuhkJZ4MHDLkL4AQwPnbpMt4desfW8Wd6FT",
      "type": "Ed25519Signature2020",
      "verificationMethod": "did:web:orb.domain2.com#umaoTg511ZzOEm23TzMFwbDv3d_gLvRr1zfZVyxXZxM"
    }
  ],
  "type": [
    "VerifiableCredential",
    "AnchorCredential"
  ]
}`

const testVCInvalidCreated = `{
  "@context": [
    "https://www.w3.org/2018/credentials/v1",
    "https://w3id.org/activityanchors/v1",
    "https://w3id.org/security/suites/jws-2020/v1",
    "https://w3id.org/security/suites/ed25519-2020/v1"
  ],
  "credentialSubject": {
    "anchor": "hl:uEiD7xzrz5lEKIq0ZZWh9ky0mNW6wxpGx_H2bxhg80c1IDA",
    "href": "hl:uEiBGozN2uP1HBNNZtL-oeg2ifE0NuKY8Bg3miVMJtVZvYQ",
    "type": ["AnchorLink"],
    "profile": "https://w3id.org/orb#v0",
    "rel": "linkset"
  },
  "id": "https://orb.domain4.com/vc/61f38a18-e2dd-4f91-b376-9586ca189b25",
  "issuanceDate": "2022-07-15T19:17:55.2446168Z",
  "issuer": "https://orb.domain4.com",
  "proof": [
    {
      "created": "hello",
      "domain": "https://orb.domain2.com",
      "proofPurpose": "assertionMethod",
      "proofValue": "z3jbefjmhKeoirfakRAtD8UPySJjsY4Hb4t6fG5myAwMr1mV4ygGKXKuhkJZ4MHDLkL4AQwPnbpMt4desfW8Wd6FT",
      "type": "Ed25519Signature2020",
      "verificationMethod": "did:web:orb.domain2.com#umaoTg511ZzOEm23TzMFwbDv3d_gLvRr1zfZVyxXZxM"
    }
  ],
  "type": [
    "VerifiableCredential",
    "AnchorCredential"
  ]
}`
