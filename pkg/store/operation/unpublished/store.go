/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package unpublished

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperledger/aries-framework-go/spi/storage"
	"github.com/trustbloc/logutil-go/pkg/log"
	"github.com/trustbloc/sidetree-core-go/pkg/api/operation"
	"github.com/trustbloc/sidetree-core-go/pkg/hashing"

	logfields "github.com/trustbloc/orb/internal/pkg/log"
	orberrors "github.com/trustbloc/orb/pkg/errors"
	"github.com/trustbloc/orb/pkg/store"
	"github.com/trustbloc/orb/pkg/store/expiry"
)

// TODO (#812) Add BDD tests to test data expiry.

const (
	nameSpace     = "unpublished-operation"
	expiryTagName = "expirationTime"
	index         = "uniqueSuffix"
	sha2_256      = 18
)

var logger = log.New("unpublished-operation-store")

// New returns a new instance of an unpublished operation store.
// This method will also register the unpublished operation store with the given expiry service which will then take
// care of deleting expired data automatically. Note that it's the caller's responsibility to start the expiry service.
// unpublishedOperationLifespan defines how long unpublished operations can stay in the store before being flagged
// for deletion.
func New(provider storage.Provider, unpublishedOperationLifespan time.Duration,
	expiryService *expiry.Service, metrics metricsProvider) (*Store, error) {
	s, err := store.Open(provider, nameSpace,
		store.NewTagGroup(index),
		store.NewTagGroup(expiryTagName),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open unpublished operation store: %w", err)
	}

	expiryService.Register(s, expiryTagName, nameSpace)

	return &Store{
		store:                        s,
		unpublishedOperationLifespan: unpublishedOperationLifespan,

		metrics: metrics,
	}, nil
}

// Store implements storage for unpublished operation.
type Store struct {
	store                        storage.Store
	unpublishedOperationLifespan time.Duration

	metrics metricsProvider
}

type metricsProvider interface {
	PutUnpublishedOperation(duration time.Duration)
	GetUnpublishedOperations(duration time.Duration)
	CalculateUnpublishedOperationKey(duration time.Duration)
}

type operationWrapper struct {
	*operation.AnchoredOperation

	ExpirationTime int64 `json:"expirationTime"`
}

// Put saves an unpublished operation. If it already exists it will be overwritten.
func (s *Store) Put(op *operation.AnchoredOperation) error {
	startTime := time.Now()

	defer func() {
		s.metrics.PutUnpublishedOperation(time.Since(startTime))
	}()

	if op.UniqueSuffix == "" {
		return fmt.Errorf("failed to save unpublished operation: suffix is empty")
	}

	opw := &operationWrapper{
		AnchoredOperation: op,
		ExpirationTime:    time.Now().Add(s.unpublishedOperationLifespan).Unix(),
	}

	opBytes, err := json.Marshal(&opw)
	if err != nil {
		return fmt.Errorf("failed to marshal unpublished operation: %w", err)
	}

	logger.Debug("Storing unpublished operation", logfields.WithOperationType(string(op.Type)),
		logfields.WithSuffix(op.UniqueSuffix), logfields.WithData(opBytes))

	tags := []storage.Tag{
		{
			Name:  index,
			Value: op.UniqueSuffix,
		},
		{
			Name:  expiryTagName,
			Value: fmt.Sprintf("%d", time.Now().Add(s.unpublishedOperationLifespan).Unix()),
		},
	}

	calculateKeyStartTime := time.Now()

	key, err := hashing.CalculateModelMultihash(op.OperationRequest, sha2_256)
	if err != nil {
		return fmt.Errorf("failed to generate key for unpublished operation for suffix[%s]: %w", op.UniqueSuffix, err)
	}

	s.metrics.CalculateUnpublishedOperationKey(time.Since(calculateKeyStartTime))

	if err := s.store.Put(key, opBytes, tags...); err != nil {
		return fmt.Errorf("failed to put unpublished operation for suffix[%s]: %w", op.UniqueSuffix, err)
	}

	return nil
}

// Get retrieves unpublished operations by suffix.
func (s *Store) Get(suffix string) ([]*operation.AnchoredOperation, error) {
	startTime := time.Now()

	defer func() {
		s.metrics.GetUnpublishedOperations(time.Since(startTime))
	}()

	var err error

	query := fmt.Sprintf("%s:%s", index, suffix)

	iter, err := s.store.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get unpublished operations for[%s]: %w", query, err)
	}

	ok, err := iter.Next()
	if err != nil {
		return nil, fmt.Errorf("iterator error for suffix[%s] : %w", suffix, err)
	}

	var ops []*operation.AnchoredOperation

	for ok {
		var value []byte

		value, err = iter.Value()
		if err != nil {
			return nil, fmt.Errorf("failed to get iterator value for suffix[%s]: %w",
				suffix, err)
		}

		var op operation.AnchoredOperation

		err = json.Unmarshal(value, &op)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal unpublished operation from store value for suffix[%s]: %w",
				suffix, err)
		}

		ops = append(ops, &op)

		ok, err = iter.Next()
		if err != nil {
			return nil, orberrors.NewTransient(fmt.Errorf("iterator error for suffix[%s] : %w", suffix, err))
		}
	}

	logger.Debug("Retrieved unpublished operations for suffix", logfields.WithTotal(len(ops)), logfields.WithSuffix(suffix))

	if len(ops) == 0 {
		return nil, fmt.Errorf("suffix[%s] not found in the unpublished operation store", suffix)
	}

	return ops, nil
}

// Delete will delete unpublished operation for suffix.
func (s *Store) Delete(op *operation.AnchoredOperation) error {
	key, err := hashing.CalculateModelMultihash(op.OperationRequest, sha2_256)
	if err != nil {
		return fmt.Errorf("failed to generate key for unpublished operation for suffix[%s]: %w", op.UniqueSuffix, err)
	}

	if err := s.store.Delete(key); err != nil {
		return fmt.Errorf("failed to delete unpublished operation with key[%s] for suffix[%s]: %w", key, op.UniqueSuffix, err)
	}

	return nil
}

// DeleteAll deletes all operations for suffixes.
func (s *Store) DeleteAll(ops []*operation.AnchoredOperation) error {
	if len(ops) == 0 {
		return nil
	}

	operations := make([]storage.Operation, len(ops))

	for i, op := range ops {
		key, err := hashing.CalculateModelMultihash(op.OperationRequest, sha2_256)
		if err != nil {
			return fmt.Errorf("failed to generate key for unpublished operation for suffix[%s]: %w", op.UniqueSuffix, err)
		}

		operations[i] = storage.Operation{Key: key}
	}

	err := s.store.Batch(operations)
	if err != nil {
		return orberrors.NewTransient(fmt.Errorf("failed to delete unpublished operations: %w", err))
	}

	logger.Debug("Deleted unpublished operations", logfields.WithTotal(len(ops)))

	return nil
}
