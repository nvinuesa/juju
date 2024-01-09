// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package bootstrap

import (
	"context"
	"fmt"

	"github.com/juju/collections/set"
	"github.com/juju/errors"
	"gopkg.in/tomb.v2"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/flags"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/objectstore"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/envcontext"
	"github.com/juju/juju/environs/space"
	"github.com/juju/juju/internal/worker/gate"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/binarystorage"
)

const (
	// States which report the state of the worker.
	stateStarted   = "started"
	stateCompleted = "completed"
)

// ControllerConfigService is the interface that is used to get the
// controller configuration.
type ControllerConfigService interface {
	ControllerConfig(context.Context) (controller.Config, error)
}

// SpaceService is the interface that is used to manage network spaces.
type SpaceService interface {
	AddSpace(ctx context.Context, name string, providerID network.Id, subnetIDs []string) (network.Id, error)
	Space(ctx context.Context, uuid string) (*network.SpaceInfo, error)
	GetAllSpaces(ctx context.Context) (network.SpaceInfos, error)
	SaveProviderSubnets(ctx context.Context, subnets []network.SubnetInfo, spaceUUID network.Id, fans network.FanConfig) error
	Remove(ctx context.Context, uuid string) error
}

// FlagService is the interface that is used to set the value of a
// flag.
type FlagService interface {
	GetFlag(context.Context, string) (bool, error)
	SetFlag(ctx context.Context, flag string, value bool, description string) error
}

// ObjectStoreGetter is the interface that is used to get a object store.
type ObjectStoreGetter interface {
	// GetObjectStore returns a object store for the given namespace.
	GetObjectStore(context.Context, string) (objectstore.ObjectStore, error)
}

// LegacyState is the interface that is used to get the legacy state (mongo).
type LegacyState interface {
	// ControllerModelUUID returns the UUID of the model that was
	// bootstrapped.  This is the only model that can have controller
	// machines.  The owner of this model is also considered "special", in
	// that they are the only user that is able to create other users
	// (until we have more fine grained permissions), and they cannot be
	// disabled.
	ControllerModelUUID() string
	// ToolsStorage returns a new binarystorage.StorageCloser that stores tools
	// metadata in the "juju" database "toolsmetadata" collection.
	ToolsStorage(store objectstore.ObjectStore) (binarystorage.StorageCloser, error)
	// AllEndpointBindingsSpaceNames returns a set of spaces names for all the
	// endpoint bindings.
	AllEndpointBindingsSpaceNames() (set.Strings, error)
	// ConstraintsBySpaceName returns all Constraints that include a positive
	// or negative space constraint for the input space name.
	ConstraintsBySpaceName(spaceName string) ([]*state.Constraints, error)
	// DefaultEndpointBindingSpace returns the current space ID to be used for
	// the default endpoint binding.
	DefaultEndpointBindingSpace() (string, error)
}

// WorkerConfig encapsulates the configuration options for the
// bootstrap worker.
type WorkerConfig struct {
	Agent                   agent.Agent
	ObjectStoreGetter       ObjectStoreGetter
	ControllerConfigService ControllerConfigService
	SpaceService            SpaceService
	FlagService             FlagService
	BootstrapUnlocker       gate.Unlocker
	AgentBinaryUploader     AgentBinaryBootstrapFunc
	Environ                 environs.BootstrapEnviron

	// Deprecated: This is only here, until we can remove the state layer.
	State LegacyState

	Logger Logger
}

// Validate ensures that the config values are valid.
func (c *WorkerConfig) Validate() error {
	if c.Agent == nil {
		return errors.NotValidf("nil Agent")
	}
	if c.ObjectStoreGetter == nil {
		return errors.NotValidf("nil ObjectStoreGetter")
	}
	if c.ControllerConfigService == nil {
		return errors.NotValidf("nil ControllerConfigService")
	}
	if c.BootstrapUnlocker == nil {
		return errors.NotValidf("nil BootstrapUnlocker")
	}
	if c.AgentBinaryUploader == nil {
		return errors.NotValidf("nil AgentBinaryUploader")
	}
	if c.FlagService == nil {
		return errors.NotValidf("nil FlagService")
	}
	if c.Logger == nil {
		return errors.NotValidf("nil Logger")
	}
	if c.State == nil {
		return errors.NotValidf("nil State")
	}
	return nil
}

type bootstrapWorker struct {
	internalStates chan string
	cfg            WorkerConfig
	tomb           tomb.Tomb
}

// NewWorker creates a new bootstrap worker.
func NewWorker(cfg WorkerConfig) (*bootstrapWorker, error) {
	return newWorker(cfg, nil)
}

func newWorker(cfg WorkerConfig, internalStates chan string) (*bootstrapWorker, error) {
	var err error
	if err = cfg.Validate(); err != nil {
		return nil, errors.Trace(err)
	}

	w := &bootstrapWorker{
		internalStates: internalStates,
		cfg:            cfg,
	}
	w.tomb.Go(w.loop)
	return w, nil
}

// Kill stops the worker.
func (w *bootstrapWorker) Kill() {
	w.tomb.Kill(nil)
}

// Wait waits for the worker to stop and then returns the reason it was
// killed.
func (w *bootstrapWorker) Wait() error {
	return w.tomb.Wait()
}

func (w *bootstrapWorker) loop() error {
	// Report the initial started state.
	w.reportInternalState(stateStarted)

	ctx, cancel := w.scopedContext()
	defer cancel()

	agentConfig := w.cfg.Agent.CurrentConfig()
	dataDir := agentConfig.DataDir()

	// Seed the agent binary to the object store.
	cleanup, err := w.seedAgentBinary(ctx, dataDir)
	if err != nil {
		return errors.Trace(err)
	}

	// Fetch spaces from substrate.
	// We need to do this before setting the API host-ports,
	// because any space names in the bootstrap machine addresses must be
	// reconcilable with space IDs at that point.
	allCtx := envcontext.WithoutCredentialInvalidator(ctx)
	if err := space.ReloadSpaces(allCtx, w.cfg.State, w.cfg.SpaceService, w.cfg.Environ); err != nil {
		if !errors.Is(err, errors.NotSupported) {
			return errors.Trace(err)
		}
		w.cfg.Logger.Debugf("Not performing spaces load on a non-networking environment")
	}

	// Set the bootstrap flag, to indicate that the bootstrap has completed.
	if err := w.cfg.FlagService.SetFlag(ctx, flags.BootstrapFlag, true, flags.BootstrapFlagDescription); err != nil {
		return errors.Trace(err)
	}

	// Cleanup only after the bootstrap flag has been set.
	cleanup()

	w.reportInternalState(stateCompleted)

	w.cfg.BootstrapUnlocker.Unlock()
	return nil
}

func (w *bootstrapWorker) reportInternalState(state string) {
	select {
	case <-w.tomb.Dying():
	case w.internalStates <- state:
	default:
	}
}

// scopedContext returns a context that is in the scope of the worker lifetime.
// It returns a cancellable context that is cancelled when the action has
// completed.
func (w *bootstrapWorker) scopedContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	return w.tomb.Context(ctx), cancel
}

func (w *bootstrapWorker) seedAgentBinary(ctx context.Context, dataDir string) (func(), error) {
	objectStore, err := w.cfg.ObjectStoreGetter.GetObjectStore(ctx, w.cfg.State.ControllerModelUUID())
	if err != nil {
		return nil, fmt.Errorf("failed to get object store: %w", err)
	}

	// Agent binary seeder will populate the tools for the agent.
	agentStorage := agentStorageShim{State: w.cfg.State}

	cleanup, err := w.cfg.AgentBinaryUploader(ctx, dataDir, agentStorage, objectStore, w.cfg.Logger)
	if err != nil {
		return nil, errors.Trace(err)
	}

	return cleanup, nil
}

type agentStorageShim struct {
	State LegacyState
}

// AgentBinaryStorage returns the interface for the BinaryAgentStorage.
// This is currently a shim wrapper around the tools storage. That will be
// renamed once we re-implement the tools storage in dqlite.
func (s agentStorageShim) AgentBinaryStorage(objectStore objectstore.ObjectStore) (BinaryAgentStorage, error) {
	return s.State.ToolsStorage(objectStore)
}
