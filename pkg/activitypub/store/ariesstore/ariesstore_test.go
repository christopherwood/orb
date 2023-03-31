/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package ariesstore_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/google/uuid"
	ariesmongodbstorage "github.com/hyperledger/aries-framework-go-ext/component/storage/mongodb"
	"github.com/hyperledger/aries-framework-go/component/storageutil/mem"
	"github.com/hyperledger/aries-framework-go/component/storageutil/mock"
	"github.com/hyperledger/aries-framework-go/spi/storage"
	"github.com/stretchr/testify/require"

	"github.com/trustbloc/orb/pkg/activitypub/store/ariesstore"
	"github.com/trustbloc/orb/pkg/activitypub/store/spi"
	"github.com/trustbloc/orb/pkg/activitypub/vocab"
	"github.com/trustbloc/orb/pkg/internal/testutil"
	"github.com/trustbloc/orb/pkg/internal/testutil/mongodbtestutil"
)

type mockStore struct {
	openStoreNameToFailOn      string
	setStoreConfigNameToFailOn string
}

func (m *mockStore) OpenStore(name string) (storage.Store, error) {
	if name == m.openStoreNameToFailOn {
		return nil, errors.New("open store error")
	}

	return nil, nil
}

func (m *mockStore) SetStoreConfig(name string, _ storage.StoreConfiguration) error {
	if name == m.setStoreConfigNameToFailOn {
		return errors.New("set store config error")
	}

	return nil
}

func (m *mockStore) GetStoreConfig(string) (storage.StoreConfiguration, error) {
	panic("implement me")
}

func (m *mockStore) GetOpenStores() []storage.Store {
	panic("implement me")
}

func (m *mockStore) Close() error {
	panic("implement me")
}

func TestNew(t *testing.T) {
	t.Run("Failed to open activities store", func(t *testing.T) {
		provider, err := ariesstore.New("ServiceName", &mockStore{
			openStoreNameToFailOn: "activity",
		}, false)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to open activity store: open store [activity]: open store error")
		require.Nil(t, provider)
	})
	t.Run("Failed to set store config on activities store", func(t *testing.T) {
		provider, err := ariesstore.New("ServiceName", &mockStore{
			setStoreConfigNameToFailOn: "activity",
		}, false)
		require.Error(t, err)
		require.Contains(t, err.Error(), "set store configuration for [activity]: set store config error")
		require.Nil(t, provider)
	})
	t.Run("Failed to open inbox store", func(t *testing.T) {
		provider, err := ariesstore.New("ServiceName", &mockStore{
			openStoreNameToFailOn: "activity-ref",
		}, true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to open reference stores: open store [activity-ref]: open store error")
		require.Nil(t, provider)
	})
	t.Run("Failed to set store config on inbox store", func(t *testing.T) {
		provider, err := ariesstore.New("ServiceName", &mockStore{
			setStoreConfigNameToFailOn: "activity-ref",
		}, true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "set store configuration for [activity-ref]: set store config error")
		require.Nil(t, provider)
	})
}

func TestFunctionalityUsingMongoDB(t *testing.T) {
	mongoDBConnString, stopMongo := mongodbtestutil.StartMongoDB(t)
	defer stopMongo()

	t.Run("Activity tests", func(t *testing.T) {
		serviceName := generateRandomServiceName()

		mongoDBProvider, err := ariesmongodbstorage.NewProvider(mongoDBConnString,
			ariesmongodbstorage.WithDBPrefix(serviceName))
		require.NoError(t, err)

		s, err := ariesstore.New(serviceName, mongoDBProvider, true)
		require.NoError(t, err)

		serviceID1 := testutil.MustParseURL("https://example.com/services/service1")
		activityID1 := testutil.MustParseURL("https://example.com/activities/activity1")
		activityID2 := testutil.MustParseURL("https://example.com/activities/activity2")
		activityID3 := testutil.MustParseURL("https://example.com/activities/activity3")

		a, err := s.GetActivity(activityID1)
		require.Error(t, err)
		require.True(t, errors.Is(err, spi.ErrNotFound))
		require.Nil(t, a)

		activity1 := vocab.NewCreateActivity(vocab.NewObjectProperty(vocab.WithIRI(serviceID1)),
			vocab.WithID(activityID1))
		require.NoError(t, s.AddActivity(activity1))

		receivedActivity1, err := s.GetActivity(activityID1)
		require.NoError(t, err)

		receivedActivity1Bytes, err := receivedActivity1.MarshalJSON()
		require.NoError(t, err)

		expectedActivity1Bytes, err := activity1.MarshalJSON()
		require.NoError(t, err)

		require.Equal(t, string(expectedActivity1Bytes), string(receivedActivity1Bytes))

		activity2 := vocab.NewAnnounceActivity(vocab.NewObjectProperty(vocab.WithIRI(serviceID1)),
			vocab.WithID(activityID2))
		require.NoError(t, s.AddActivity(activity2))

		activity3 := vocab.NewCreateActivity(vocab.NewObjectProperty(vocab.WithIRI(serviceID1)),
			vocab.WithID(activityID3))
		require.NoError(t, s.AddActivity(activity3))

		// Before adding references, confirm that a query by reference returns no results
		it, err := s.QueryActivities(
			spi.NewCriteria(spi.WithReferenceType(spi.Inbox), spi.WithObjectIRI(serviceID1)))
		require.NoError(t, err)
		require.NotNil(t, it)

		checkActivityQueryResultsInOrder(t, it, 0)

		require.NoError(t, s.AddReference(spi.Inbox, serviceID1, activityID1))
		require.NoError(t, s.AddReference(spi.Inbox, serviceID1, activityID2))
		require.NoError(t, s.AddReference(spi.Inbox, serviceID1, activityID3))

		t.Run("Query all", func(t *testing.T) {
			t.Run("Ascending (default) order", func(t *testing.T) {
				it, err := s.QueryActivities(spi.NewCriteria())
				require.EqualError(t, err, "unsupported query criteria")
				require.Nil(t, it)
			})
		})

		t.Run("Query by reference", func(t *testing.T) {
			t.Run("Ascending (default) order", func(t *testing.T) {
				t.Run("Default page size", func(t *testing.T) {
					it, err := s.QueryActivities(
						spi.NewCriteria(spi.WithReferenceType(spi.Inbox), spi.WithObjectIRI(serviceID1)))
					require.NoError(t, err)
					require.NotNil(t, it)

					checkActivityQueryResultsInOrder(t, it, 3, activityID1, activityID2, activityID3)
				})
				t.Run("Page size 2", func(t *testing.T) {
					it, err := s.QueryActivities(
						spi.NewCriteria(spi.WithReferenceType(spi.Inbox), spi.WithObjectIRI(serviceID1)),
						spi.WithPageSize(2))
					require.NoError(t, err)
					require.NotNil(t, it)

					// Note that the expected total items is still 3, despite the different page size.
					// Total items is based on the total matching references.
					checkActivityQueryResultsInOrder(t, it, 3, activityID1, activityID2)
				})
			})
			t.Run("Descending order", func(t *testing.T) {
				it, err := s.QueryActivities(
					spi.NewCriteria(spi.WithReferenceType(spi.Inbox), spi.WithObjectIRI(serviceID1)),
					spi.WithSortOrder(spi.SortDescending))
				require.NoError(t, err)
				require.NotNil(t, it)

				checkActivityQueryResultsInOrder(t, it, 3, activityID3, activityID2, activityID1)
			})
			t.Run("Fail to get total items from reference iterator", func(t *testing.T) {
				mockAriesStore, err := ariesstore.New(serviceName, &mock.Provider{
					OpenStoreReturn: &mock.Store{
						QueryReturn: &mock.Iterator{
							ErrTotalItems: errors.New("total items error"),
						},
					},
				}, true)
				require.NoError(t, err)

				it, err := mockAriesStore.QueryActivities(
					spi.NewCriteria(spi.WithReferenceType(spi.Inbox), spi.WithObjectIRI(serviceID1)))
				require.EqualError(t, err,
					"failed to get total items from reference iterator: total items error")
				require.Nil(t, it)
			})
		})
	})
	t.Run("Reference tests", func(t *testing.T) {
		serviceName := generateRandomServiceName()

		mongoDBProvider, err := ariesmongodbstorage.NewProvider(mongoDBConnString,
			ariesmongodbstorage.WithDBPrefix(serviceName))
		require.NoError(t, err)

		s, err := ariesstore.New(serviceName, mongoDBProvider, true)
		require.NoError(t, err)

		actor1 := testutil.MustParseURL("https://actor1")
		actor2 := testutil.MustParseURL("https://actor2")
		actor3 := testutil.MustParseURL("https://actor3")
		actor4 := testutil.MustParseURL("https://actor4")

		it, err := s.QueryReferences(spi.Follower, spi.NewCriteria())
		require.EqualError(t, err, "object IRI is required")
		require.Nil(t, it)

		it, err = s.QueryReferences(spi.Follower, spi.NewCriteria(spi.WithObjectIRI(actor1)))
		require.NoError(t, err)
		require.NotNil(t, it)

		checkReferenceQueryResultsInOrder(t, it, 0)

		require.NoError(t, s.AddReference(spi.Follower, actor1, actor2))
		require.NoError(t, s.AddReference(spi.Follower, actor1, actor3))

		it, err = s.QueryReferences(spi.Follower, spi.NewCriteria(spi.WithObjectIRI(actor1)))
		require.NoError(t, err)

		checkReferenceQueryResultsInOrder(t, it, 2, actor2, actor3)

		// Try the same query as above, but in descending order this time
		it, err = s.QueryReferences(spi.Follower, spi.NewCriteria(spi.WithObjectIRI(actor1)),
			spi.WithSortOrder(spi.SortDescending))
		require.NoError(t, err)

		checkReferenceQueryResultsInOrder(t, it, 2, actor3, actor2)

		it, err = s.QueryReferences(spi.Following, spi.NewCriteria(spi.WithObjectIRI(actor1)))
		require.NoError(t, err)

		checkReferenceQueryResultsInOrder(t, it, 0)

		require.NoError(t, s.AddReference(spi.Following, actor1, actor2))

		it, err = s.QueryReferences(spi.Following, spi.NewCriteria(spi.WithObjectIRI(actor1)))
		require.NoError(t, err)

		checkReferenceQueryResultsInOrder(t, it, 1, actor2)

		require.NoError(t, s.DeleteReference(spi.Follower, actor1, actor2))

		it, err = s.QueryReferences(spi.Follower, spi.NewCriteria(spi.WithObjectIRI(actor1)))
		require.NoError(t, err)

		checkReferenceQueryResultsInOrder(t, it, 1, actor3)

		it, err = s.QueryReferences(spi.Follower, spi.NewCriteria(spi.WithObjectIRI(actor2)))
		require.NoError(t, err)

		checkReferenceQueryResultsInOrder(t, it, 0)

		require.NoError(t, s.AddReference(spi.Follower, actor2, actor3))

		it, err = s.QueryReferences(spi.Follower, spi.NewCriteria(spi.WithObjectIRI(actor2)))
		require.NoError(t, err)

		checkReferenceQueryResultsInOrder(t, it, 1, actor3)

		it, err = s.QueryReferences(spi.Follower,
			spi.NewCriteria(spi.WithObjectIRI(actor2), spi.WithReferenceIRI(actor3)))
		require.NoError(t, err)

		checkReferenceQueryResultsInOrder(t, it, 1, actor3)

		// Now try doing a query using both object IRI and activity type. Since none of the data was added with an
		// activity type, we should get no matches at this point.
		it, err = s.QueryReferences(spi.Follower,
			spi.NewCriteria(spi.WithObjectIRI(actor2), spi.WithType(vocab.TypeCreate)))
		require.NoError(t, err)

		checkReferenceQueryResultsInOrder(t, it, 0)

		require.NoError(t, s.AddReference(spi.Follower, actor2, actor4, spi.WithActivityType(vocab.TypeCreate)))

		// Now that we've added a reference with activity type metadata, we should get one match (the one added above)
		it, err = s.QueryReferences(spi.Follower,
			spi.NewCriteria(spi.WithObjectIRI(actor2), spi.WithType(vocab.TypeCreate)))
		require.NoError(t, err)

		checkReferenceQueryResultsInOrder(t, it, 1, actor4)
	})
}

func TestStore_Activity_Failures(t *testing.T) {
	t.Run("Fail to add activity", func(t *testing.T) {
		provider, err := ariesstore.New("ServiceName", &mock.Provider{
			OpenStoreReturn: &mock.Store{
				ErrPut: errors.New("put error"),
			},
		}, false)
		require.NoError(t, err)

		serviceID1 := testutil.MustParseURL("https://example.com/services/service1")

		activityID1 := testutil.MustParseURL("https://example.com/activities/activity1")

		err = provider.AddActivity(vocab.NewCreateActivity(vocab.NewObjectProperty(vocab.WithIRI(serviceID1)),
			vocab.WithID(activityID1)))
		require.EqualError(t, err, "failed to store activity: put error")
	})
	t.Run("Fail to get activity", func(t *testing.T) {
		provider, err := ariesstore.New("ServiceName", &mock.Provider{
			OpenStoreReturn: &mock.Store{
				ErrGet: errors.New("get error"),
			},
		}, false)
		require.NoError(t, err)

		_, err = provider.GetActivity(testutil.MustParseURL("https://example.com/activities/activity1"))
		require.EqualError(t, err, "unexpected failure while getting activity from store: get error")
	})
	t.Run("Fail to query", func(t *testing.T) {
		serviceID1 := testutil.MustParseURL("https://example.com/services/service1")

		provider, err := ariesstore.New("ServiceName", &mock.Provider{
			OpenStoreReturn: &mock.Store{
				ErrQuery: errors.New("query error"),
			},
		}, true)
		require.NoError(t, err)

		_, err = provider.QueryActivities(spi.NewCriteria(spi.WithObjectIRI(serviceID1), spi.WithReferenceType(spi.Inbox)))
		require.EqualError(t, err, "failed to query store: query error")
	})
	t.Run("Unsupported query criteria", func(t *testing.T) {
		provider, err := ariesstore.New("ServiceName", mem.NewProvider(), false)
		require.NoError(t, err)

		serviceID1 := testutil.MustParseURL("https://example.com/services/service1")

		_, err = provider.QueryActivities(spi.NewCriteria(spi.WithObjectIRI(serviceID1),
			spi.WithActivityIRIs(testutil.MustParseURL("https://example.com/activities/activity1"),
				testutil.MustParseURL("https://example.com/activities/activity1"))))
		require.EqualError(t, err, "unsupported query criteria")
	})
}

func TestStore_Reference_Failures(t *testing.T) {
	t.Run("Fail to add reference", func(t *testing.T) {
		t.Run("Fail to store in underlying storage", func(t *testing.T) {
			provider, err := ariesstore.New("ServiceName", &mock.Provider{
				OpenStoreReturn: &mock.Store{
					ErrPut: errors.New("put error"),
				},
			}, false)
			require.NoError(t, err)

			actor1 := testutil.MustParseURL("https://actor1")
			actor2 := testutil.MustParseURL("https://actor2")

			err = provider.AddReference(spi.Following, actor1, actor2)
			require.EqualError(t, err, "failed to store reference: put error")
		})
	})
	t.Run("Fail to delete reference", func(t *testing.T) {
		t.Run("Fail to delete in underlying storage", func(t *testing.T) {
			provider, err := ariesstore.New("ServiceName", &mock.Provider{
				OpenStoreReturn: &mock.Store{
					ErrDelete: errors.New("delete error"),
				},
			}, false)
			require.NoError(t, err)

			actor1 := testutil.MustParseURL("https://actor1")
			actor2 := testutil.MustParseURL("https://actor2")

			err = provider.DeleteReference(spi.Following, actor1, actor2)
			require.EqualError(t, err, "failed to delete reference: delete error")
		})
	})
	t.Run("Fail to query references", func(t *testing.T) {
		t.Run("Fail to query in underlying storage", func(t *testing.T) {
			provider, err := ariesstore.New("ServiceName", &mock.Provider{
				OpenStoreReturn: &mock.Store{
					ErrQuery: errors.New("query error"),
				},
			}, true)
			require.NoError(t, err)

			actor1 := testutil.MustParseURL("https://actor1")

			_, err = provider.QueryReferences(spi.Following, spi.NewCriteria(spi.WithObjectIRI(actor1)))
			require.EqualError(t, err, "failed to query store: query error")
		})
		t.Run("Fail to query with both object IRI and activity type", func(t *testing.T) {
			provider, err := ariesstore.New("ServiceName", mem.NewProvider(), false)
			require.NoError(t, err)

			actor1 := testutil.MustParseURL("https://actor1")

			it, err := provider.QueryReferences(spi.Follower,
				spi.NewCriteria(spi.WithObjectIRI(actor1), spi.WithType(vocab.TypeCreate)))
			require.EqualError(t, err, "cannot run query since the underlying storage provider "+
				"does not support querying with multiple tags")
			require.Nil(t, it)
		})
	})
}

// expectedActivities is with respect to the query's page settings.
// Since Iterator.TotalItems' count is not affected by page settings, expectedTotalItems must be passed in explicitly.
// It can't be determined by looking at the length of expectedActivities.
func checkActivityQueryResultsInOrder(t *testing.T, it spi.ActivityIterator, expectedTotalItems int, expectedActivities ...*url.URL) {
	t.Helper()

	require.NotNil(t, it)

	for i := 0; i < len(expectedActivities); i++ {
		retrievedActivity, err := it.Next()
		require.NoError(t, err)
		require.NotNil(t, retrievedActivity)
		require.Equal(t, expectedActivities[i].String(), retrievedActivity.ID().URL().String())
	}

	totalItems, err := it.TotalItems()
	require.NoError(t, err)
	require.Equal(t, expectedTotalItems, totalItems)

	retrievedActivity, err := it.Next()
	require.Error(t, err)
	require.True(t, errors.Is(err, spi.ErrNotFound))
	require.Nil(t, retrievedActivity)
}

// expectedIRIs is with respect to the query's page settings.
// Since Iterator.TotalItems' count is not affected by page settings, expectedTotalItems must be passed in explicitly.
// It can't be determined by looking at the length of expectedIRIs.
func checkReferenceQueryResultsInOrder(t *testing.T, it spi.ReferenceIterator, expectedTotalItems int, expectedIRIs ...*url.URL) {
	t.Helper()

	require.NotNil(t, it)

	for i := 0; i < len(expectedIRIs); i++ {
		iri, err := it.Next()
		require.NoError(t, err)
		require.NotNil(t, iri)
		require.Equal(t, expectedIRIs[i].String(), iri.String())
	}

	totalItems, err := it.TotalItems()
	require.NoError(t, err)
	require.Equal(t, expectedTotalItems, totalItems)

	iri, err := it.Next()
	require.Error(t, err)
	require.True(t, errors.Is(err, spi.ErrNotFound))
	require.Nil(t, iri)

	require.NoError(t, it.Close())
}

func generateRandomServiceName() string {
	return uuid.NewString() + "_"
}
