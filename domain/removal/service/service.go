// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"
	"time"

	"github.com/juju/clock"

	"github.com/juju/juju/core/changestream"
	"github.com/juju/juju/core/logger"
	corerelation "github.com/juju/juju/core/relation"
	"github.com/juju/juju/core/watcher"
	relationerrors "github.com/juju/juju/domain/relation/errors"
	"github.com/juju/juju/domain/removal"
	"github.com/juju/juju/internal/errors"
)

// State describes retrieval and persistence methods for entity removal.
type State interface {

	// RelationExists returns true if a relation exists with the input UUID.
	RelationExists(ctx context.Context, rUUID string) (bool, error)

	// RelationAdvanceLifeAndScheduleRemoval advances the life cycle of the
	// relation with the input UUID to dying if it is alive, and schedules a
	// removal job for the relation, qualified with the input force boolean.
	RelationAdvanceLifeAndScheduleRemoval(
		ctx context.Context, removalUUID, relUUID string, force bool, when time.Time,
	) error

	// NamespaceForWatchRemovals returns the table name whose UUIDs we
	// are watching in order to be notified of new removal jobs.
	NamespaceForWatchRemovals() string
}

// WatcherFactory describes methods for creating watchers.
type WatcherFactory interface {
	// NewUUIDsWatcher returns a watcher that emits the UUIDs for changes to the
	// input table name that match the input mask.
	NewUUIDsWatcher(tableName string, changeMask changestream.ChangeType) (watcher.StringsWatcher, error)
}

// Service provides the API for working with entity removal.
type Service struct {
	st State

	clock  clock.Clock
	logger logger.Logger
}

// RemoveRelation checks if a relation with the input UUID exists.
// If it does, the relation is guaranteed after this call to be:
// - No longer alive.
// - Removed or scheduled to be removed with the input force qualification.
// The UUID for the scheduled removal job is returned.
// [relationerrors.RelationNotFound] is returned if no such relation exists.
func (s *Service) RemoveRelation(ctx context.Context, relUUID corerelation.UUID, force bool) (removal.UUID, error) {
	exists, err := s.st.RelationExists(ctx, relUUID.String())
	if err != nil {
		return "", errors.Errorf("checking if relation %q exists: %w", relUUID, err)
	}
	if !exists {
		return "", errors.Errorf("relation %q does not exist", relUUID).Add(relationerrors.RelationNotFound)
	}

	jobUUID, err := removal.NewUUID()
	if err != nil {
		return "", errors.Capture(err)
	}

	if err := s.st.RelationAdvanceLifeAndScheduleRemoval(
		ctx, jobUUID.String(), relUUID.String(), force, s.clock.Now().UTC(),
	); err != nil {
		return "", errors.Errorf("removing relation %q: %w", relUUID, err)
	}

	s.logger.Infof(ctx, "scheduled removal job %q for relation %q", jobUUID, relUUID)
	return jobUUID, nil
}

// WatchableService provides the API for working with entity removal,
// including the ability to create watchers.
type WatchableService struct {
	Service
	watcherFactory WatcherFactory
}

// NewWatchableService creates a new WatchableService
// for working with entity removal.
func NewWatchableService(
	st State,
	watcherFactory WatcherFactory,
	clock clock.Clock,
	logger logger.Logger,
) *WatchableService {
	return &WatchableService{
		Service: Service{
			st:     st,
			clock:  clock,
			logger: logger,
		},
		watcherFactory: watcherFactory,
	}
}

// WatchRemovals watches for scheduled removal jobs.
// The returned watcher emits the UUIDs of any inserted or updated jobs.
func (s *WatchableService) WatchRemovals() (watcher.StringsWatcher, error) {
	w, err := s.watcherFactory.NewUUIDsWatcher(s.st.NamespaceForWatchRemovals(), changestream.Changed)
	if err != nil {
		return nil, errors.Errorf("creating watcher for removals: %w", err)
	}
	return w, nil
}
