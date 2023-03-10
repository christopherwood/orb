/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package handler

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/trustbloc/logutil-go/pkg/log"

	logfields "github.com/trustbloc/orb/internal/pkg/log"
	orberrors "github.com/trustbloc/orb/pkg/errors"
)

var logger = log.New("log-monitor-handler")

// New creates new proof handler.
func New(store logMonitorStore, logResolver logResolver) *Handler {
	return &Handler{
		store:       store,
		logResolver: logResolver,
	}
}

// Handler handles registering/un-registering log for monitoring.
type Handler struct {
	store       logMonitorStore
	logResolver logResolver
}

type logMonitorStore interface {
	Activate(logURL string) error
	Deactivate(logURL string) error
}

type logResolver interface {
	ResolveLog(uri string) (*url.URL, error)
}

// Accept will get actor's log to the list of logs to be monitored by log monitoring service.
func (h *Handler) Accept(actor *url.URL) error { //nolint:dupl
	logger.Debug("Received request to add log for actor", logfields.WithActorIRI(actor))

	logURL, err := h.logResolver.ResolveLog(actor.String())
	if err != nil {
		if errors.Is(err, orberrors.ErrContentNotFound) {
			logger.Info("Actor doesn't have a log.", logfields.WithActorIRI(actor))

			return nil
		}

		return fmt.Errorf("failed to resolve log for actor[%s]: %w", actor, err)
	}

	logger.Debug("Retrieved log URL for actor.", logfields.WithLogURL(logURL), logfields.WithActorIRI(actor))

	err = h.store.Activate(logURL.String())
	if err != nil {
		return fmt.Errorf("failed to add logURL[%s] for monitoring: %w", logURL, err)
	}

	logger.Debug("Added log URL for monitoring.", logfields.WithLogURL(logURL))

	return nil
}

// Undo will deactivate actor's log. It will remove actor's log from the list of logs
// to be monitored by log monitoring service.
func (h *Handler) Undo(actor *url.URL) error { //nolint:dupl
	logger.Debug("Received request to deactivate log for actor", logfields.WithActorIRI(actor))

	logURL, err := h.logResolver.ResolveLog(actor.String())
	if err != nil {
		if errors.Is(err, orberrors.ErrContentNotFound) {
			logger.Info("Actor doesn't have a log.", logfields.WithActorIRI(actor))

			return nil
		}

		return fmt.Errorf("failed to resolve log for actor[%s]: %w", actor, err)
	}

	logger.Debug("Retrieved log URL for actor", logfields.WithLogURL(logURL), logfields.WithActorIRI(actor))

	err = h.store.Deactivate(logURL.String())
	if err != nil {
		return fmt.Errorf("failed to deactiveate logURL[%s] for monitoring: %w", logURL, err)
	}

	logger.Debug("Deactivated log URL for monitoring.", logfields.WithLogURL(logURL))

	return nil
}
