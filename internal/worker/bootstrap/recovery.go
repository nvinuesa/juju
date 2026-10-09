// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package bootstrap

import (
	"context"
	"os"

	"github.com/juju/clock"
	jujuerrors "github.com/juju/errors"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/core/logger"
	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/unit"
	machineerrors "github.com/juju/juju/domain/machine/errors"
	environsbootstrap "github.com/juju/juju/environs/bootstrap"
	"github.com/juju/juju/internal/bootstrap"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/errors"
)

// readBootstrapParams reads the bootstrap parameters written by the
// bootstrap client into the agent's data directory.
func readBootstrapParams(dataDir string) (instancecfg.StateInitializationParams, error) {
	bootstrapParamsData, err := os.ReadFile(bootstrap.BootstrapParamsPath(dataDir))
	if err != nil {
		return instancecfg.StateInitializationParams{}, errors.Errorf("reading bootstrap params file: %w", err)
	}
	var args instancecfg.StateInitializationParams
	if err := args.Unmarshal(bootstrapParamsData); err != nil {
		return instancecfg.StateInitializationParams{}, errors.Capture(err)
	}
	return args, nil
}

// setControllerApplicationPassword sets the controller application's
// password. It is a no-op when bootstrap generated no password.
func setControllerApplicationPassword(
	ctx context.Context,
	applicationService ApplicationService,
	agentPasswordService AgentPasswordService,
	password string,
) error {
	if password == "" {
		return nil
	}
	applicationUUID, err := applicationService.GetApplicationUUIDByName(
		ctx, environsbootstrap.ControllerApplicationName,
	)
	if err != nil {
		return errors.Errorf("getting controller application UUID: %w", err)
	}
	if err := agentPasswordService.SetApplicationPassword(
		ctx, applicationUUID, password,
	); err != nil {
		return errors.Errorf("setting controller application password: %w", err)
	}
	return nil
}

// RecoveryBootstrapConfig contains the dependencies for recovering a
// controller from a recovery archive.
type RecoveryBootstrapConfig struct {
	// RemoveBootstrapSSHKeys removes the bootstrap-only SSH keys from the
	// machine.
	RemoveBootstrapSSHKeys RemoveBootstrapSSHKeysFunc

	ControllerConfigService ControllerConfigService
	ControllerNodeService   ControllerNodeService
	AgentPasswordService    AgentPasswordService
	ApplicationService      ApplicationService
	MachineService          MachineService
	NetworkService          NetworkService
	ControllerModel         coremodel.Model
	BootstrapAddressFinder  BootstrapAddressFinderFunc
	DataDir                 string
	APIPort                 int
	AgentFinalizer          AgentFinalizerFunc
	AgentPassword           string
	ApplicationPassword     string
	UnitPassword            string
	ServiceManagerGetter    ServiceManagerGetterFunc
	Logger                  logger.Logger
	Clock                   clock.Clock
}

// Validate ensures that the config values are valid.
func (c *RecoveryBootstrapConfig) Validate() error {
	if c.ControllerConfigService == nil {
		return jujuerrors.NotValidf("nil ControllerConfigService")
	}
	if c.ControllerNodeService == nil {
		return jujuerrors.NotValidf("nil ControllerNodeService")
	}
	if c.AgentPasswordService == nil {
		return jujuerrors.NotValidf("nil AgentPasswordService")
	}
	if c.ApplicationService == nil {
		return jujuerrors.NotValidf("nil ApplicationService")
	}
	if c.MachineService == nil {
		return jujuerrors.NotValidf("nil MachineService")
	}
	if c.NetworkService == nil {
		return jujuerrors.NotValidf("nil NetworkService")
	}
	if c.DataDir == "" {
		return jujuerrors.NotValidf("missing DataDir")
	}
	if c.APIPort == 0 {
		return jujuerrors.NotValidf("missing APIPort")
	}
	if c.AgentFinalizer == nil {
		return jujuerrors.NotValidf("nil AgentFinalizer")
	}
	if c.AgentPassword == "" {
		return jujuerrors.NotValidf("missing AgentPassword")
	}
	if c.BootstrapAddressFinder == nil {
		return jujuerrors.NotValidf("nil BootstrapAddressFinder")
	}
	if c.ControllerModel.ModelType == coremodel.CAAS && c.ServiceManagerGetter == nil {
		return jujuerrors.NotValidf("nil ServiceManagerGetter")
	}
	if err := c.ControllerModel.UUID.Validate(); err != nil {
		return errors.Errorf("controller model id: %w", err)
	}
	if c.Logger == nil {
		return jujuerrors.NotValidf("nil Logger")
	}
	if c.Clock == nil {
		return jujuerrors.NotValidf("nil Clock")
	}
	return nil
}

type recoveryBootstrap struct {
	cfg    RecoveryBootstrapConfig
	logger logger.Logger
}

// NewRecoveryBootstrap constructs the operation that finishes recovering a
// controller from the archive loaded during agent bootstrap. Construction
// validates dependencies; the operation performs the work.
func NewRecoveryBootstrap(cfg RecoveryBootstrapConfig) (Operation, error) {
	if err := cfg.Validate(); err != nil {
		return nil, errors.Capture(err)
	}
	b := &recoveryBootstrap{cfg: cfg, logger: cfg.Logger.Child("worker")}
	return b.run, nil
}

func (b *recoveryBootstrap) run(ctx context.Context) (func(), error) {
	params, err := readBootstrapParams(b.cfg.DataDir)
	if err != nil {
		return nil, errors.Errorf("getting bootstrap params: %w", err)
	}
	if params.RecoveryArchivePath == "" {
		return nil, errors.New("recovery bootstrap selected without a recovery archive")
	}

	controllerConfig, err := b.cfg.ControllerConfigService.ControllerConfig(ctx)
	if err != nil {
		return nil, errors.Capture(err)
	}

	// Retrieve controller addresses needed to set the API host ports.
	bootstrapAddresses, err := b.cfg.BootstrapAddressFinder(ctx, params.BootstrapMachineInstanceId)
	if err != nil {
		return nil, errors.Capture(err)
	}

	// The controller application and unit come from the archive holding
	// the source's credentials. Bridge them to the passwords bootstrap
	// generated for the replacement pod, which the Kubernetes
	// application secret carries, so the controller charm container can
	// introduce itself and log in.
	if err := setControllerApplicationPassword(
		ctx, b.cfg.ApplicationService, b.cfg.AgentPasswordService, b.cfg.ApplicationPassword,
	); err != nil {
		return nil, errors.Capture(err)
	}
	if err := b.setControllerUnitPassword(ctx); err != nil {
		return nil, errors.Capture(err)
	}

	// Finalise the agent by either setting the machine as provisioned
	// or by setting the controller node password.
	if err := b.cfg.AgentFinalizer(
		ctx, b.cfg.AgentPasswordService, b.cfg.MachineService, params, b.cfg.AgentPassword,
	); err != nil {
		if errors.Is(err, machineerrors.MachineCloudInstanceAlreadyExists) {
			// The recovery stage already patched the archived
			// controller machine's cloud instance onto the
			// replacement; the finalizer's insert is redundant.
			b.logger.Debugf(ctx, "machine cloud instance already recovered: %v", err)
		} else {
			return nil, errors.Errorf("finalising agent: %w", err)
		}
	}

	// Recovery publishes the addresses of the recovered controller 0,
	// the same controller ID fresh bootstrap creates.
	if err := InitialiseAPIHostPorts(ctx, b.cfg.ControllerNodeService,
		b.cfg.NetworkService, b.cfg.ServiceManagerGetter,
		agent.BootstrapControllerId, b.cfg.ControllerModel.ModelType,
		controllerConfig, bootstrapAddresses, b.cfg.APIPort); err != nil {
		b.logger.Errorf(ctx, "unable to set API host ports %v:%w", bootstrapAddresses, err)
		return nil, errors.Capture(err)
	}

	if err := b.cfg.RemoveBootstrapSSHKeys(params.BootstrapSSHAuthorizedKeys); err != nil {
		return nil, errors.Errorf("removing bootstrap SSH keys: %w", err)
	}

	// Recovery seeds no agent binary at the worker level, so nothing is
	// staged for cleanup; the archive bundle dir is removed inside the
	// agent bootstrap's recovery stage. Identity seeding (macaroon
	// config, initial users, storage pools, spaces, the controller
	// charm and authorized keys) is skipped: the archived databases
	// provide it.
	return nil, nil
}

// setControllerUnitPassword bridges the recovered controller unit's
// password to the one bootstrap generated for the replacement's
// controller pod, which the Kubernetes application secret carries. It is
// a no-op when bootstrap provided no unit password (IAAS).
func (b *recoveryBootstrap) setControllerUnitPassword(ctx context.Context) error {
	if b.cfg.UnitPassword == "" {
		return nil
	}
	controllerUnit, err := unit.NewNameFromParts(environsbootstrap.ControllerApplicationName, 0)
	if err != nil {
		return errors.Errorf("creating controller unit name: %w", err)
	}
	if err := b.cfg.AgentPasswordService.SetUnitPassword(ctx, controllerUnit, b.cfg.UnitPassword); err != nil {
		return errors.Errorf("setting controller unit password: %w", err)
	}
	return nil
}
