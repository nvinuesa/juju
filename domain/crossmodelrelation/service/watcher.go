// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"
	"fmt"

	"github.com/juju/collections/set"
	"github.com/juju/worker/v4"
	"github.com/juju/worker/v4/catacomb"

	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/internal/errors"
)

// secretRevisionWatcher is a watcher that watches for secret revision changes.
type secretRevisionWatcher struct {
	catacomb catacomb.Catacomb
	logger   logger.Logger

	sourceWatcher  watcher.StringsWatcher
	processChanges func(ctx context.Context, events ...string) ([]watcher.SecretTriggerChange, error)

	out chan []watcher.SecretTriggerChange
}

func newSecretRevisionWatcher(
	sourceWatcher watcher.StringsWatcher, logger logger.Logger,
	processChanges func(ctx context.Context, events ...string) ([]watcher.SecretTriggerChange, error),
) (*secretRevisionWatcher, error) {
	w := &secretRevisionWatcher{
		sourceWatcher:  sourceWatcher,
		logger:         logger,
		processChanges: processChanges,
		out:            make(chan []watcher.SecretTriggerChange),
	}
	err := catacomb.Invoke(catacomb.Plan{
		Name: "secret-revision-watcher",
		Site: &w.catacomb,
		Work: w.loop,
		Init: []worker.Worker{sourceWatcher},
	})
	return w, errors.Capture(err)
}

func (w *secretRevisionWatcher) scopedContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(w.catacomb.Context(context.Background()))
}

func (w *secretRevisionWatcher) loop() error {
	defer close(w.out)

	var (
		historyIDs set.Strings
		changes    []watcher.SecretTriggerChange
	)
	// To allow the initial event to be sent.
	out := w.out
	addChanges := func(events set.Strings) error {
		if len(events) == 0 {
			return nil
		}
		ctx, cancel := w.scopedContext()
		processed, err := w.processChanges(ctx, events.Values()...)
		cancel()
		if err != nil {
			return errors.Capture(err)
		}
		if len(processed) == 0 {
			return nil
		}

		if historyIDs == nil {
			historyIDs = set.NewStrings()
		}
		for _, v := range processed {
			id := fmt.Sprintf("%s/%d", v.URI, v.Revision)
			if historyIDs.Contains(id) {
				continue
			}
			changes = append(changes, v)
			historyIDs.Add(id)
		}

		out = w.out
		return nil
	}

	for {
		select {
		case <-w.catacomb.Dying():
			return w.catacomb.ErrDying()
		case events, ok := <-w.sourceWatcher.Changes():
			if !ok {
				return errors.Errorf("event watcher closed")
			}
			if err := addChanges(set.NewStrings(events...)); err != nil {
				return errors.Capture(err)
			}
		case out <- changes:
			changes = nil
			out = nil
		}
	}
}

// Changes returns the channel of secret changes.
func (w *secretRevisionWatcher) Changes() <-chan []watcher.SecretTriggerChange {
	return w.out
}

// Stop stops the watcher.
func (w *secretRevisionWatcher) Stop() error {
	w.Kill()
	return w.Wait()
}

// Kill kills the watcher via its tomb.
func (w *secretRevisionWatcher) Kill() {
	w.catacomb.Kill(nil)
}

// Wait waits for the watcher's tomb to die,
// and returns the error with which it was killed.
func (w *secretRevisionWatcher) Wait() error {
	return w.catacomb.Wait()
}
