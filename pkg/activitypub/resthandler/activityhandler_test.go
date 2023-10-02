/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package resthandler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/hyperledger/aries-framework-go/component/storageutil/mock"
	"github.com/stretchr/testify/require"

	apmocks "github.com/trustbloc/orb/pkg/activitypub/mocks"
	"github.com/trustbloc/orb/pkg/activitypub/service/mocks"
	"github.com/trustbloc/orb/pkg/activitypub/store/ariesstore"
	"github.com/trustbloc/orb/pkg/activitypub/store/memstore"
	"github.com/trustbloc/orb/pkg/activitypub/store/spi"
	"github.com/trustbloc/orb/pkg/activitypub/vocab"
	orberrors "github.com/trustbloc/orb/pkg/errors"
	"github.com/trustbloc/orb/pkg/internal/testutil"
)

//go:generate counterfeiter -o ../mocks/activityiterator.gen.go --fake-name ActivityIterator ../store/spi ActivityIterator

const (
	inboxURL  = "https://example.com/services/orb/inbox"
	outboxURL = "https://example.com/services/orb/outbox"
	sharesURL = "https://example.com/services/orb/shares"
)

func TestNewOutbox(t *testing.T) {
	cfg := &Config{
		BasePath:           basePath,
		ObjectIRI:          serviceIRI,
		ServiceEndpointURL: serviceIRI,
		PageSize:           4,
	}

	h := NewOutbox(cfg, memstore.New(""), &mocks.SignatureVerifier{}, spi.SortDescending, &apmocks.AuthTokenMgr{})
	require.NotNil(t, h)
	require.Equal(t, "/services/orb/outbox", h.Path())
	require.Equal(t, http.MethodGet, h.Method())
	require.NotNil(t, h.Handler())

	objectIRI, err := h.getObjectIRI(nil)
	require.NoError(t, err)
	require.NotNil(t, objectIRI)
	require.Equal(t, "https://example1.com/services/orb", objectIRI.String())

	id, err := h.getID(objectIRI, nil)
	require.NoError(t, err)
	require.NotNil(t, id)
	require.Equal(t, "https://example1.com/services/orb/outbox", id.String())
}

func TestNewInbox(t *testing.T) {
	cfg := &Config{
		BasePath:           basePath,
		ObjectIRI:          serviceIRI,
		ServiceEndpointURL: serviceIRI,
		PageSize:           4,
	}

	h := NewInbox(cfg, memstore.New(""), &mocks.SignatureVerifier{}, spi.SortDescending, &apmocks.AuthTokenMgr{})
	require.NotNil(t, h)
	require.Equal(t, "/services/orb/inbox", h.Path())
	require.Equal(t, http.MethodGet, h.Method())
	require.NotNil(t, h.Handler())

	objectIRI, err := h.getObjectIRI(nil)
	require.NoError(t, err)
	require.NotNil(t, objectIRI)
	require.Equal(t, "https://example1.com/services/orb", objectIRI.String())

	id, err := h.getID(objectIRI, nil)
	require.NoError(t, err)
	require.NotNil(t, id)
	require.Equal(t, "https://example1.com/services/orb/inbox", id.String())
}

func TestNewShares(t *testing.T) {
	const id = "http://example1.com/vc/31027ffa-bfc9-4a36-aa1a-6bfc04e6d432"

	cfg := &Config{
		BasePath:           basePath,
		ObjectIRI:          serviceIRI,
		ServiceEndpointURL: serviceIRI,
	}

	h := NewShares(cfg, memstore.New(""), &mocks.SignatureVerifier{}, spi.SortDescending, &apmocks.AuthTokenMgr{})
	require.NotNil(t, h)
	require.Equal(t, "/services/orb/shares/{id}", h.Path())
	require.Equal(t, http.MethodGet, h.Method())
	require.NotNil(t, h.Handler())

	t.Run("Success", func(t *testing.T) {
		restore := setIDParam(id)
		defer restore()

		objectIRI, err := h.getObjectIRI(nil)
		require.NoError(t, err)
		require.NotNil(t, objectIRI)
		require.Equal(t, id, objectIRI.String())

		actualID, err := h.getID(objectIRI, nil)
		require.NoError(t, err)
		require.NotNil(t, actualID)
		require.Equal(t,
			serviceIRI.String()+"/shares/http%3A%2F%2Fexample1.com%2Fvc%2F31027ffa-bfc9-4a36-aa1a-6bfc04e6d432",
			actualID.String())
	})

	t.Run("No ID in URL -> error", func(t *testing.T) {
		restore := setIDParam("")
		defer restore()

		objectIRI, err := h.getObjectIRI(nil)
		require.EqualError(t, err, "id not specified in URL")
		require.Nil(t, objectIRI)
	})
}

func TestNewLikes(t *testing.T) {
	const id = "http://example1.com/vc/31027ffa-bfc9-4a36-aa1a-6bfc04e6d432"

	cfg := &Config{
		BasePath:           basePath,
		ObjectIRI:          serviceIRI,
		ServiceEndpointURL: serviceIRI,
	}

	h := NewLikes(cfg, memstore.New(""), &mocks.SignatureVerifier{}, spi.SortDescending, &apmocks.AuthTokenMgr{})
	require.NotNil(t, h)
	require.Equal(t, "/services/orb/likes/{id}", h.Path())
	require.Equal(t, http.MethodGet, h.Method())
	require.NotNil(t, h.Handler())

	t.Run("Success", func(t *testing.T) {
		restore := setIDParam(id)
		defer restore()

		objectIRI, err := h.getObjectIRI(nil)
		require.NoError(t, err)
		require.NotNil(t, objectIRI)
		require.Equal(t, id, objectIRI.String())

		actualID, err := h.getID(objectIRI, nil)
		require.NoError(t, err)
		require.NotNil(t, actualID)
		require.Equal(t,
			serviceIRI.String()+"/likes/http%3A%2F%2Fexample1.com%2Fvc%2F31027ffa-bfc9-4a36-aa1a-6bfc04e6d432",
			actualID.String())
	})

	t.Run("No ID in URL -> error", func(t *testing.T) {
		restore := setIDParam("")
		defer restore()

		objectIRI, err := h.getObjectIRI(nil)
		require.EqualError(t, err, "id not specified in URL")
		require.Nil(t, objectIRI)
	})
}

func TestActivities_Handler(t *testing.T) {
	activityStore := memstore.New("")

	for _, activity := range newMockCreateActivities(19) {
		require.NoError(t, activityStore.AddActivity(activity))
		require.NoError(t, activityStore.AddReference(spi.Inbox, serviceIRI, activity.ID().URL()))
	}

	require.NoError(t, activityStore.AddReference(spi.Follower, serviceIRI, service2IRI))

	cfg := &Config{
		BasePath:           basePath,
		ObjectIRI:          serviceIRI,
		ServiceEndpointURL: serviceIRI,
		PageSize:           4,
	}

	verifier := &mocks.SignatureVerifier{}
	verifier.VerifyRequestReturns(true, service2IRI, nil)

	t.Run("Success", func(t *testing.T) {
		h := NewInbox(cfg, activityStore, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, inboxURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusOK, result.StatusCode)

		respBytes, err := io.ReadAll(result.Body)
		require.NoError(t, err)

		t.Logf("%s", respBytes)

		require.Equal(t, testutil.GetCanonical(t, inboxJSON), testutil.GetCanonical(t, string(respBytes)))
		require.NoError(t, result.Body.Close())
	})

	t.Run("Store error", func(t *testing.T) {
		errExpected := fmt.Errorf("injected store error")

		s := &mocks.ActivityStore{}
		s.QueryReferencesReturns(nil, errExpected)

		h := NewInbox(cfg, s, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, inboxURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusInternalServerError, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})

	t.Run("Marshal error", func(t *testing.T) {
		h := NewInbox(cfg, activityStore, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		errExpected := fmt.Errorf("injected marshal error")

		h.marshal = func(v interface{}) ([]byte, error) {
			return nil, errExpected
		}

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, inboxURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusInternalServerError, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})

	t.Run("GetObjectIRI error", func(t *testing.T) {
		h := NewInbox(cfg, activityStore, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		t.Run("Internal error", func(t *testing.T) {
			errExpected := fmt.Errorf("injected error")

			h.getObjectIRI = func(req *http.Request) (*url.URL, error) {
				return nil, errExpected
			}

			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, inboxURL, http.NoBody)

			h.handle(rw, req)

			result := rw.Result()
			require.Equal(t, http.StatusInternalServerError, result.StatusCode)
			require.NoError(t, result.Body.Close())
		})

		t.Run("Bad request", func(t *testing.T) {
			errExpected := fmt.Errorf("injected error")

			h.getObjectIRI = func(req *http.Request) (*url.URL, error) {
				return nil, orberrors.NewBadRequest(errExpected)
			}

			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, inboxURL, http.NoBody)

			h.handle(rw, req)

			result := rw.Result()
			require.Equal(t, http.StatusBadRequest, result.StatusCode)
			require.NoError(t, result.Body.Close())
		})
	})

	t.Run("GetID error", func(t *testing.T) {
		h := NewInbox(cfg, activityStore, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		errExpected := fmt.Errorf("injected error")

		h.getID = func(*url.URL, *http.Request) (*url.URL, error) {
			return nil, errExpected
		}

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, inboxURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusInternalServerError, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})

	t.Run("Verify signature error", func(t *testing.T) {
		errExpected := errors.New("injected verifier error")

		verifier := &mocks.SignatureVerifier{}
		verifier.VerifyRequestReturns(false, nil, errExpected)

		tm := &apmocks.AuthTokenMgr{}
		tm.RequiredAuthTokensReturns([]string{"admin", "read"}, nil)

		h := NewInbox(cfg, activityStore, verifier, spi.SortDescending, tm)
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, inboxURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusInternalServerError, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})

	t.Run("Invalid signature", func(t *testing.T) {
		verifier := &mocks.SignatureVerifier{}
		verifier.VerifyRequestReturns(false, nil, nil)

		tm := &apmocks.AuthTokenMgr{}
		tm.RequiredAuthTokensReturns([]string{"admin", "read"}, nil)

		h := NewInbox(cfg, activityStore, verifier, spi.SortDescending, tm)
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, inboxURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusUnauthorized, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})
}

func TestActivities_PageHandler(t *testing.T) {
	activityStore := memstore.New("")

	verifier := &mocks.SignatureVerifier{}
	verifier.VerifyRequestReturns(true, serviceIRI, nil)

	for _, activity := range newMockCreateActivities(19) {
		require.NoError(t, activityStore.AddActivity(activity))
		require.NoError(t, activityStore.AddReference(spi.Inbox, serviceIRI, activity.ID().URL()))
	}

	t.Run("First page -> Success", func(t *testing.T) {
		handleActivitiesRequest(t, serviceIRI, activityStore, "true", "", inboxFirstPageJSON)
	})

	t.Run("Page by num -> Success", func(t *testing.T) {
		handleActivitiesRequest(t, serviceIRI, activityStore, "true", "3", inboxPage3JSON)
	})

	t.Run("Page num too large -> Success", func(t *testing.T) {
		handleActivitiesRequest(t, serviceIRI, activityStore, "true", "30", inboxPageTooLargeJSON)
	})

	t.Run("Last page -> Success", func(t *testing.T) {
		handleActivitiesRequest(t, serviceIRI, activityStore, "true", "0", inboxLastPageJSON)
	})

	t.Run("Invalid page-num -> Success", func(t *testing.T) {
		handleActivitiesRequest(t, serviceIRI, activityStore, "true", "invalid", inboxFirstPageJSON)
	})

	t.Run("Invalid page -> Success", func(t *testing.T) {
		handleActivitiesRequest(t, serviceIRI, activityStore, "invalid", "3", inboxJSON)
	})

	t.Run("Store error", func(t *testing.T) {
		errExpected := fmt.Errorf("injected store error")

		s := &mocks.ActivityStore{}
		s.QueryActivitiesReturns(nil, errExpected)

		cfg := &Config{
			ObjectIRI: serviceIRI,
			PageSize:  4,
		}

		h := NewOutbox(cfg, s, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		restorePaging := setPaging(h.handler, "true", "0")
		defer restorePaging()

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, outboxURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusInternalServerError, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})

	t.Run("Marshal error", func(t *testing.T) {
		cfg := &Config{
			ObjectIRI: serviceIRI,
			PageSize:  4,
		}

		h := NewOutbox(cfg, activityStore, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		restorePaging := setPaging(h.handler, "true", "0")
		defer restorePaging()

		errExpected := fmt.Errorf("injected marshal error")

		h.marshal = func(v interface{}) ([]byte, error) {
			return nil, errExpected
		}

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, outboxURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusInternalServerError, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})
}

func TestReadOutbox_Handler(t *testing.T) {
	activityStore := memstore.New("")

	for _, activity := range newMockCreateActivities(14) {
		require.NoError(t, activityStore.AddActivity(activity))
		require.NoError(t, activityStore.AddReference(spi.Outbox, serviceIRI, activity.ID().URL()))
	}

	for _, activity := range newMockCreateActivities(5) {
		require.NoError(t, activityStore.AddActivity(activity))
		require.NoError(t, activityStore.AddReference(spi.Outbox, serviceIRI, activity.ID().URL()))
		require.NoError(t, activityStore.AddReference(spi.PublicOutbox, serviceIRI, activity.ID().URL()))
	}

	require.NoError(t, activityStore.AddReference(spi.Follower, serviceIRI, service2IRI))

	cfg := &Config{
		BasePath:           basePath,
		ObjectIRI:          serviceIRI,
		ServiceEndpointURL: serviceIRI,
		PageSize:           4,
	}

	t.Run("Authorized -> All items", func(t *testing.T) {
		verifier := &mocks.SignatureVerifier{}
		verifier.VerifyRequestReturns(true, service2IRI, nil)

		h := NewOutbox(cfg, activityStore, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, outboxURL, http.NoBody)

		h.handleOutbox(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusOK, result.StatusCode)

		respBytes, err := io.ReadAll(result.Body)
		require.NoError(t, err)

		t.Logf("%s", respBytes)

		require.Equal(t, testutil.GetCanonical(t, outboxJSON), testutil.GetCanonical(t, string(respBytes)))
		require.NoError(t, result.Body.Close())
	})

	t.Run("Unauthorized -> Public items", func(t *testing.T) {
		verifier := &mocks.SignatureVerifier{}
		verifier.VerifyRequestReturns(false, nil, nil)

		tm := &apmocks.AuthTokenMgr{}
		tm.RequiredAuthTokensReturns([]string{"admin", "read"}, nil)

		h := NewOutbox(cfg, activityStore, verifier, spi.SortDescending, tm)
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, outboxURL, http.NoBody)

		h.handleOutbox(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusOK, result.StatusCode)

		respBytes, err := io.ReadAll(result.Body)
		require.NoError(t, err)

		t.Logf("%s", respBytes)

		require.Equal(t, testutil.GetCanonical(t, publicOutboxJSON), testutil.GetCanonical(t, string(respBytes)))
		require.NoError(t, result.Body.Close())
	})

	t.Run("Authorization error", func(t *testing.T) {
		verifier := &mocks.SignatureVerifier{}
		verifier.VerifyRequestReturns(false, nil, errors.New("injected auth error"))

		tm := &apmocks.AuthTokenMgr{}
		tm.RequiredAuthTokensReturns([]string{"admin", "read"}, nil)

		h := NewOutbox(cfg, activityStore, verifier, spi.SortDescending, tm)
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, outboxURL, http.NoBody)

		h.handleOutbox(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusInternalServerError, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})
}

func TestShares_Handler(t *testing.T) {
	const id = "https://sally.example.com/transactions/d607506e-6964-4991-a19f-674952380760"

	srvcIRI := testutil.MustParseURL("https://sally.example.com/services/orb")

	objectIRI := testutil.MustParseURL(id)

	shares := newMockActivities(vocab.TypeAnnounce, 19, func(i int) string {
		return fmt.Sprintf("https://example%d.com/activities/announce_activity_%d", i, i)
	})

	activityStore := memstore.New("")

	for _, a := range shares {
		require.NoError(t, activityStore.AddActivity(a))
		require.NoError(t, activityStore.AddReference(spi.Share, objectIRI, a.ID().URL()))
	}

	cfg := &Config{
		BasePath:           basePath,
		ObjectIRI:          srvcIRI,
		ServiceEndpointURL: srvcIRI,
		PageSize:           4,
	}

	verifier := &mocks.SignatureVerifier{}
	verifier.VerifyRequestReturns(true, srvcIRI, nil)

	t.Run("Success", func(t *testing.T) {
		h := NewShares(cfg, activityStore, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		restore := setIDParam(id)
		defer restore()

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, sharesURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusOK, result.StatusCode)

		respBytes, err := io.ReadAll(result.Body)
		require.NoError(t, err)

		t.Logf("%s", respBytes)

		require.Equal(t, testutil.GetCanonical(t, sharesJSON), testutil.GetCanonical(t, string(respBytes)))
		require.NoError(t, result.Body.Close())
	})
}

func TestShares_PageHandler(t *testing.T) {
	const id = "https://sally.example.com/transactions/d607506e-6964-4991-a19f-674952380760"

	srvcIRI := testutil.MustParseURL("https://sally.example.com/services/orb")

	objectIRI := testutil.MustParseURL(id)

	shares := newMockActivities(vocab.TypeAnnounce, 19, func(i int) string {
		return fmt.Sprintf("https://example%d.com/activities/announce_activity_%d", i, i)
	})

	activityStore := memstore.New("")

	for _, a := range shares {
		require.NoError(t, activityStore.AddActivity(a))
		require.NoError(t, activityStore.AddReference(spi.Share, objectIRI, a.ID().URL()))
	}

	cfg := &Config{
		BasePath:           basePath,
		ObjectIRI:          srvcIRI,
		ServiceEndpointURL: srvcIRI,
		PageSize:           4,
	}

	verifier := &mocks.SignatureVerifier{}
	verifier.VerifyRequestReturns(true, srvcIRI, nil)

	t.Run("First page -> Success", func(t *testing.T) {
		h := NewShares(cfg, activityStore, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		restorePaging := setPaging(h.handler, "true", "")
		defer restorePaging()

		restore := setIDParam(id)
		defer restore()

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, sharesURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusOK, result.StatusCode)

		respBytes, err := io.ReadAll(result.Body)
		require.NoError(t, err)

		t.Logf("%s", respBytes)

		require.Equal(t, testutil.GetCanonical(t, sharesFirstPageJSON), testutil.GetCanonical(t, string(respBytes)))
		require.NoError(t, result.Body.Close())
	})

	t.Run("By page -> Success", func(t *testing.T) {
		h := NewShares(cfg, activityStore, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		restorePaging := setPaging(h.handler, "true", "1")
		defer restorePaging()

		restore := setIDParam(id)
		defer restore()

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, sharesURL, http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusOK, result.StatusCode)

		respBytes, err := io.ReadAll(result.Body)
		require.NoError(t, err)

		t.Logf("%s", respBytes)

		require.Equal(t, testutil.GetCanonical(t, sharesPage1JSON), testutil.GetCanonical(t, string(respBytes)))
		require.NoError(t, result.Body.Close())
	})
}

func TestLiked_Handler(t *testing.T) {
	liked := newMockActivities(vocab.TypeLike, 19, func(i int) string {
		return fmt.Sprintf("https://example%d.com/activities/like_activity_%d", i, i)
	})

	activityStore := memstore.New("")

	for _, a := range liked {
		require.NoError(t, activityStore.AddActivity(a))
		require.NoError(t, activityStore.AddReference(spi.Liked, serviceIRI, a.Object().AnchorEvent().URL()[0]))
	}

	cfg := &Config{
		BasePath:           basePath,
		ObjectIRI:          serviceIRI,
		ServiceEndpointURL: serviceIRI,
		PageSize:           5,
	}

	verifier := &mocks.SignatureVerifier{}
	verifier.VerifyRequestReturns(true, serviceIRI, nil)

	h := NewLiked(cfg, activityStore, verifier, &apmocks.AuthTokenMgr{})
	require.NotNil(t, h)

	t.Run("Main page -> Success", func(t *testing.T) {
		handleRequest(t, h.handler, h.handle, "false", "", likedJSON)
	})

	t.Run("First page -> Success", func(t *testing.T) {
		handleRequest(t, h.handler, h.handle, "true", "", likedFirstPageJSON)
	})
}

func TestNewActivity(t *testing.T) {
	h := NewActivity(&Config{BasePath: basePath}, memstore.New(""), &mocks.SignatureVerifier{},
		spi.SortDescending, &apmocks.AuthTokenMgr{})
	require.NotNil(t, h)
	require.Equal(t, basePath+ActivitiesPath, h.Path())
	require.Equal(t, http.MethodGet, h.Method())
	require.NotNil(t, h.Handler())
}

func TestActivity_Handler(t *testing.T) {
	id := "abd35f29-032f-4e22-8f52-df00365323bc"
	publicID := "bcd35f29-032f-4e22-8f52-df00365323bc"

	cfg := &Config{
		ObjectIRI:          serviceIRI,
		ServiceEndpointURL: serviceIRI,
		BasePath:           basePath,
	}

	activityStore := memstore.New("")

	require.NoError(t, activityStore.AddActivity(newMockActivity(vocab.TypeCreate,
		testutil.NewMockID(serviceIRI, fmt.Sprintf("/activities/%s", id)))))

	require.NoError(t, activityStore.AddActivity(newMockActivity(vocab.TypeCreate,
		testutil.NewMockID(serviceIRI, fmt.Sprintf("/activities/%s", publicID)), vocab.PublicIRI)))

	t.Run("Success", func(t *testing.T) {
		verifier := &mocks.SignatureVerifier{}
		verifier.VerifyRequestReturns(true, nil, nil)

		h := NewActivity(cfg, activityStore, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, serviceIRI.String(), http.NoBody)

		restoreID := setIDParam(id)
		defer restoreID()

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusOK, result.StatusCode)

		respBytes, err := io.ReadAll(result.Body)
		require.NoError(t, err)

		t.Logf("%s", respBytes)

		require.Equal(t, testutil.GetCanonical(t, activityJSON), testutil.GetCanonical(t, string(respBytes)))
		require.NoError(t, result.Body.Close())
	})

	t.Run("No activity ID -> BadRequest", func(t *testing.T) {
		h := NewActivity(cfg, activityStore, &mocks.SignatureVerifier{}, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, serviceIRI.String(), http.NoBody)

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusBadRequest, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})

	t.Run("Activity ID not found -> NotFound", func(t *testing.T) {
		h := NewActivity(cfg, activityStore, &mocks.SignatureVerifier{}, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, serviceIRI.String(), http.NoBody)

		restoreID := setIDParam("123")
		defer restoreID()

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusNotFound, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})

	t.Run("Store error", func(t *testing.T) {
		as := &mocks.ActivityStore{}
		as.GetActivityReturns(nil, errors.New("injected store error"))

		h := NewActivity(cfg, as, &mocks.SignatureVerifier{}, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, serviceIRI.String(), http.NoBody)

		restoreID := setIDParam(id)
		defer restoreID()

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusInternalServerError, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})

	t.Run("Marshal error", func(t *testing.T) {
		h := NewActivity(cfg, activityStore, &mocks.SignatureVerifier{}, spi.SortDescending, &apmocks.AuthTokenMgr{})
		require.NotNil(t, h)

		errExpected := fmt.Errorf("injected marshal error")

		h.marshal = func(v interface{}) ([]byte, error) {
			return nil, errExpected
		}

		rw := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, serviceIRI.String(), http.NoBody)

		restoreID := setIDParam(id)
		defer restoreID()

		h.handle(rw, req)

		result := rw.Result()
		require.Equal(t, http.StatusInternalServerError, result.StatusCode)
		require.NoError(t, result.Body.Close())
	})

	t.Run("Unauthorized", func(t *testing.T) {
		verifier := &mocks.SignatureVerifier{}
		verifier.VerifyRequestReturns(true, nil, nil)

		cnfg := &Config{
			BasePath:               basePath,
			ObjectIRI:              serviceIRI,
			ServiceEndpointURL:     serviceIRI,
			VerifyActorInSignature: true,
		}

		tm := &apmocks.AuthTokenMgr{}
		tm.RequiredAuthTokensReturns([]string{"read"}, nil)

		h := NewActivity(cnfg, activityStore, verifier, spi.SortDescending, tm)
		require.NotNil(t, h)

		t.Run("Non-public activity -> unauthorized", func(t *testing.T) {
			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, serviceIRI.String(), http.NoBody)

			restoreID := setIDParam(id)
			defer restoreID()

			h.handle(rw, req)

			result := rw.Result()
			require.Equal(t, http.StatusUnauthorized, result.StatusCode)
			require.NoError(t, result.Body.Close())
		})

		t.Run("Public activity -> success", func(t *testing.T) {
			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, serviceIRI.String(), http.NoBody)

			restoreID := setIDParam(publicID)
			defer restoreID()

			h.handle(rw, req)

			result := rw.Result()
			require.Equal(t, http.StatusOK, result.StatusCode)

			respBytes, err := io.ReadAll(result.Body)
			require.NoError(t, err)

			t.Logf("%s", respBytes)

			require.Equal(t, testutil.GetCanonical(t, publicActivityJSON), testutil.GetCanonical(t, string(respBytes)))
			require.NoError(t, result.Body.Close())
		})

		t.Run("Auth error", func(t *testing.T) {
			errExpected := fmt.Errorf("injected auth error")

			verifier := &mocks.SignatureVerifier{}
			verifier.VerifyRequestReturns(false, nil, errExpected)

			h := NewActivity(cnfg, activityStore, verifier, spi.SortDescending, tm)
			require.NotNil(t, h)

			rw := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, serviceIRI.String(), http.NoBody)

			restoreID := setIDParam(id)
			defer restoreID()

			h.handle(rw, req)

			result := rw.Result()
			require.Equal(t, http.StatusInternalServerError, result.StatusCode)
			require.NoError(t, result.Body.Close())
		})
	})
}

func TestGetActivities(t *testing.T) {
	store, err := ariesstore.New("", &mock.Provider{
		OpenStoreReturn: &mock.Store{
			QueryReturn: &mock.Iterator{ErrTotalItems: errors.New("total items error")},
		},
	}, true)
	require.NoError(t, err)

	activitiesHandler := Activities{handler: &handler{AuthHandler: &AuthHandler{activityStore: store}}}

	activities, err := activitiesHandler.getActivities(&url.URL{}, &url.URL{}, spi.Inbox)
	require.EqualError(t, err, "failed to get total items from reference query: total items error")
	require.Nil(t, activities)
}

func TestActivityHandlerGetPage(t *testing.T) {
	mit := &apmocks.ActivityIterator{}
	mit.NextReturnsOnCall(0, nil, nil)
	mit.NextReturnsOnCall(0, nil, spi.ErrNotFound)
	mit.TotalItemsReturns(0, errors.New("total items error"))

	mockActivityStore := mocks.ActivityStore{}
	mockActivityStore.QueryActivitiesReturns(mit, nil)

	activitiesHandler := Activities{handler: &handler{AuthHandler: &AuthHandler{activityStore: &mockActivityStore}}}

	page, err := activitiesHandler.getPage(&url.URL{}, &url.URL{}, spi.Inbox)
	require.EqualError(t, err, "failed to get total items from activity query: total items error")
	require.Nil(t, page)
}

func handleActivitiesRequest(t *testing.T, serviceIRI *url.URL, as spi.Store, page, pageNum, expected string) {
	t.Helper()

	cfg := &Config{
		ObjectIRI:          serviceIRI,
		ServiceEndpointURL: serviceIRI,
		PageSize:           4,
	}

	verifier := &mocks.SignatureVerifier{}
	verifier.VerifyRequestReturns(true, serviceIRI, nil)

	h := NewInbox(cfg, as, verifier, spi.SortDescending, &apmocks.AuthTokenMgr{})
	require.NotNil(t, h)

	restorePaging := setPaging(h.handler, page, pageNum)
	defer restorePaging()

	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, outboxURL, http.NoBody)

	h.handle(rw, req)

	result := rw.Result()
	require.Equal(t, http.StatusOK, result.StatusCode)

	respBytes, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	require.NoError(t, result.Body.Close())

	t.Logf("%s", respBytes)

	require.Equal(t, testutil.GetCanonical(t, expected), testutil.GetCanonical(t, string(respBytes)))
}

func newMockActivities(t vocab.Type, num int, getURI func(i int) string) []*vocab.ActivityType {
	activities := make([]*vocab.ActivityType, num)

	for i := 0; i < num; i++ {
		activities[i] = newMockActivity(t, testutil.MustParseURL(getURI(i)))
	}

	return activities
}

func newMockActivity(t vocab.Type, id *url.URL, to ...*url.URL) *vocab.ActivityType {
	if t == vocab.TypeAnnounce {
		return vocab.NewAnnounceActivity(
			vocab.NewObjectProperty(vocab.WithIRI(id)),
			vocab.WithID(id),
			vocab.WithTo(to...),
		)
	}

	if t == vocab.TypeLike {
		actor := testutil.MustParseURL("https://example1.com/services/orb")
		ref := testutil.MustParseURL("hl:uEiCsFp-ft8tI1DFGbXs78tw-HS561mMPa3Z6GsGAHElrNQ:uoQ-CeE1odHRwczovL3NhbGx5LmV4YW1wbGUuY29tL2Nhcy91RWlDc0ZwLWZ0OHRJMURGR2JYczc4dHctSFM1NjFtTVBhM1o2R3NHQUhFbHJOUXhCaXBmczovL2JhZmtyZWlmbWMycHo3bjZsamRrZGNydG5wbTU3ZnhiNmR1eGh2dnRkYjV2eG02cTJ5Z2FieXNsbGd1")
		additionalRef := testutil.MustParseURL("hl:uEiCsFp-ft8tI1DFGbXs78tw-JS571mMPa3Z6GsGAHElrNQ:uoQ-CeE1odHRwczovL3NhbGx5LmV4YW1wbGUuY29tL2Nhcy91RWlDc0ZwLWZ0OHRJMURGR2JYczc4dHctSFM1NjFtTVBhM1o2R3NHQUhFbHJOUXhCaXBmczovL2JhZmtyZWlmbWMycHo3bjZsamRrZGNydG5wbTU3ZnhiNmR1eGh2dnRkYjV2eG02cTJ5Z2FieXNsbGd1")

		publishedTime := getStaticTime()

		return vocab.NewLikeActivity(
			vocab.NewObjectProperty(
				vocab.WithAnchorEvent(
					vocab.NewAnchorEvent(nil, vocab.WithURL(ref)),
				),
			),
			vocab.WithID(id),
			vocab.WithActor(actor),
			vocab.WithTo(to...),
			vocab.WithPublishedTime(&publishedTime),
			vocab.WithResult(
				vocab.NewObjectProperty(
					vocab.WithAnchorEvent(
						vocab.NewAnchorEvent(nil, vocab.WithURL(additionalRef)),
					),
				),
			),
		)
	}

	return vocab.NewCreateActivity(vocab.NewObjectProperty(
		vocab.WithIRI(testutil.MustParseURL("http://sally.example.com/transactions/bafkreihwsn"))),
		vocab.WithID(id),
		vocab.WithTo(to...),
	)
}

func getStaticTime() time.Time {
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		panic(err)
	}

	return time.Date(2021, time.January, 27, 9, 30, 10, 0, loc)
}

const (
	outboxJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://example1.com/services/orb/outbox",
  "type": "OrderedCollection",
  "totalItems": 19,
  "first": "https://example1.com/services/orb/outbox?page=true",
  "last": "https://example1.com/services/orb/outbox?page=true&page-num=0"
}`

	publicOutboxJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://example1.com/services/orb/outbox",
  "type": "OrderedCollection",
  "totalItems": 5,
  "first": "https://example1.com/services/orb/outbox?page=true",
  "last": "https://example1.com/services/orb/outbox?page=true&page-num=0"
}`

	inboxJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://example1.com/services/orb/inbox",
  "type": "OrderedCollection",
  "totalItems": 19,
  "first": "https://example1.com/services/orb/inbox?page=true",
  "last": "https://example1.com/services/orb/inbox?page=true&page-num=0"
}`

	inboxFirstPageJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://example1.com/services/orb/inbox?page=true&page-num=4",
  "next": "https://example1.com/services/orb/inbox?page=true&page-num=3",
  "orderedItems": [
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_18",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_17",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_16",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_15",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    }
  ],
  "totalItems": 19,
  "type": "OrderedCollectionPage"
}`

	inboxLastPageJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://example1.com/services/orb/inbox?page=true&page-num=0",
  "orderedItems": [
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_2",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_1",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_0",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    }
  ],
  "prev": "https://example1.com/services/orb/inbox?page=true&page-num=1",
  "totalItems": 19,
  "type": "OrderedCollectionPage"
}`

	inboxPage3JSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://example1.com/services/orb/inbox?page=true&page-num=3",
  "next": "https://example1.com/services/orb/inbox?page=true&page-num=2",
  "orderedItems": [
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_14",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_13",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_12",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://activity_11",
      "object": {
        "@context": "https://w3id.org/activityanchors/v1",
        "type": "AnchorEvent",
        "url": "hl:uEiCJWrCq8ttsWob5UVueRQiQ_QUrocJY6ZA8BDgzgakuhg:uoQ-BeEJpcGZzOi8vYmFma3JlaWVqbGt5a3Y0dzNucm5pbjZrcmxvcGVrY2VxN3Vjc3hpb2NsZHV6YXBhZWhhenlka2pvcXk"
      },
      "type": "Create"
    }
  ],
  "prev": "https://example1.com/services/orb/inbox?page=true&page-num=4",
  "totalItems": 19,
  "type": "OrderedCollectionPage"
}`

	inboxPageTooLargeJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://example1.com/services/orb/inbox?page=true&page-num=30",
  "next": "https://example1.com/services/orb/inbox?page=true&page-num=4",
  "totalItems": 19,
  "type": "OrderedCollectionPage"
}`

	sharesJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "first": "https://sally.example.com/services/orb/shares/https%3A%2F%2Fsally.example.com%2Ftransactions%2Fd607506e-6964-4991-a19f-674952380760?page=true",
  "id": "https://sally.example.com/services/orb/shares/https%3A%2F%2Fsally.example.com%2Ftransactions%2Fd607506e-6964-4991-a19f-674952380760",
  "last": "https://sally.example.com/services/orb/shares/https%3A%2F%2Fsally.example.com%2Ftransactions%2Fd607506e-6964-4991-a19f-674952380760?page=true&page-num=0",
  "totalItems": 19,
  "type": "OrderedCollection"
}`

	sharesFirstPageJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://sally.example.com/services/orb/shares/https%3A%2F%2Fsally.example.com%2Ftransactions%2Fd607506e-6964-4991-a19f-674952380760?page=true&page-num=4",
  "type": "OrderedCollectionPage",
  "next": "https://sally.example.com/services/orb/shares/https%3A%2F%2Fsally.example.com%2Ftransactions%2Fd607506e-6964-4991-a19f-674952380760?page=true&page-num=3",
  "totalItems": 19,
  "orderedItems": [
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://example18.com/activities/announce_activity_18",
      "object": "https://example18.com/activities/announce_activity_18",
      "type": "Announce"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://example17.com/activities/announce_activity_17",
      "object": "https://example17.com/activities/announce_activity_17",
      "type": "Announce"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://example16.com/activities/announce_activity_16",
      "object": "https://example16.com/activities/announce_activity_16",
      "type": "Announce"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://example15.com/activities/announce_activity_15",
      "object": "https://example15.com/activities/announce_activity_15",
      "type": "Announce"
    }
  ]
}`

	sharesPage1JSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://sally.example.com/services/orb/shares/https%3A%2F%2Fsally.example.com%2Ftransactions%2Fd607506e-6964-4991-a19f-674952380760?page=true&page-num=1",
  "type": "OrderedCollectionPage",
  "next": "https://sally.example.com/services/orb/shares/https%3A%2F%2Fsally.example.com%2Ftransactions%2Fd607506e-6964-4991-a19f-674952380760?page=true&page-num=0",
  "prev": "https://sally.example.com/services/orb/shares/https%3A%2F%2Fsally.example.com%2Ftransactions%2Fd607506e-6964-4991-a19f-674952380760?page=true&page-num=2",
  "totalItems": 19,
  "orderedItems": [
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://example6.com/activities/announce_activity_6",
      "object": "https://example6.com/activities/announce_activity_6",
      "type": "Announce"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://example5.com/activities/announce_activity_5",
      "object": "https://example5.com/activities/announce_activity_5",
      "type": "Announce"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://example4.com/activities/announce_activity_4",
      "object": "https://example4.com/activities/announce_activity_4",
      "type": "Announce"
    },
    {
      "@context": "https://www.w3.org/ns/activitystreams",
      "id": "https://example3.com/activities/announce_activity_3",
      "object": "https://example3.com/activities/announce_activity_3",
      "type": "Announce"
    }
  ]
}`

	likedJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "first": "https://example1.com/services/orb/liked?page=true",
  "id": "https://example1.com/services/orb/liked",
  "last": "https://example1.com/services/orb/liked?page=true&page-num=3",
  "totalItems": 19,
  "type": "OrderedCollection"
}`

	likedFirstPageJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://example1.com/services/orb/liked?page=true&page-num=0",
  "next": "https://example1.com/services/orb/liked?page=true&page-num=1",
  "orderedItems": [
    "hl:uEiCsFp-ft8tI1DFGbXs78tw-HS561mMPa3Z6GsGAHElrNQ:uoQ-CeE1odHRwczovL3NhbGx5LmV4YW1wbGUuY29tL2Nhcy91RWlDc0ZwLWZ0OHRJMURGR2JYczc4dHctSFM1NjFtTVBhM1o2R3NHQUhFbHJOUXhCaXBmczovL2JhZmtyZWlmbWMycHo3bjZsamRrZGNydG5wbTU3ZnhiNmR1eGh2dnRkYjV2eG02cTJ5Z2FieXNsbGd1",
    "hl:uEiCsFp-ft8tI1DFGbXs78tw-HS561mMPa3Z6GsGAHElrNQ:uoQ-CeE1odHRwczovL3NhbGx5LmV4YW1wbGUuY29tL2Nhcy91RWlDc0ZwLWZ0OHRJMURGR2JYczc4dHctSFM1NjFtTVBhM1o2R3NHQUhFbHJOUXhCaXBmczovL2JhZmtyZWlmbWMycHo3bjZsamRrZGNydG5wbTU3ZnhiNmR1eGh2dnRkYjV2eG02cTJ5Z2FieXNsbGd1",
    "hl:uEiCsFp-ft8tI1DFGbXs78tw-HS561mMPa3Z6GsGAHElrNQ:uoQ-CeE1odHRwczovL3NhbGx5LmV4YW1wbGUuY29tL2Nhcy91RWlDc0ZwLWZ0OHRJMURGR2JYczc4dHctSFM1NjFtTVBhM1o2R3NHQUhFbHJOUXhCaXBmczovL2JhZmtyZWlmbWMycHo3bjZsamRrZGNydG5wbTU3ZnhiNmR1eGh2dnRkYjV2eG02cTJ5Z2FieXNsbGd1",
    "hl:uEiCsFp-ft8tI1DFGbXs78tw-HS561mMPa3Z6GsGAHElrNQ:uoQ-CeE1odHRwczovL3NhbGx5LmV4YW1wbGUuY29tL2Nhcy91RWlDc0ZwLWZ0OHRJMURGR2JYczc4dHctSFM1NjFtTVBhM1o2R3NHQUhFbHJOUXhCaXBmczovL2JhZmtyZWlmbWMycHo3bjZsamRrZGNydG5wbTU3ZnhiNmR1eGh2dnRkYjV2eG02cTJ5Z2FieXNsbGd1",
    "hl:uEiCsFp-ft8tI1DFGbXs78tw-HS561mMPa3Z6GsGAHElrNQ:uoQ-CeE1odHRwczovL3NhbGx5LmV4YW1wbGUuY29tL2Nhcy91RWlDc0ZwLWZ0OHRJMURGR2JYczc4dHctSFM1NjFtTVBhM1o2R3NHQUhFbHJOUXhCaXBmczovL2JhZmtyZWlmbWMycHo3bjZsamRrZGNydG5wbTU3ZnhiNmR1eGh2dnRkYjV2eG02cTJ5Z2FieXNsbGd1"
  ],
  "totalItems": 19,
  "type": "OrderedCollectionPage"
}`

	activityJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://example1.com/services/orb/activities/abd35f29-032f-4e22-8f52-df00365323bc",
  "object": "http://sally.example.com/transactions/bafkreihwsn",
  "type": "Create"
}`

	publicActivityJSON = `{
  "@context": "https://www.w3.org/ns/activitystreams",
  "id": "https://example1.com/services/orb/activities/bcd35f29-032f-4e22-8f52-df00365323bc",
  "object": "http://sally.example.com/transactions/bafkreihwsn",
  "type": "Create",
  "to": "https://www.w3.org/ns/activitystreams#Public"
}`
)
