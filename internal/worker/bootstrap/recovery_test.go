// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	stdtesting "testing"

	"github.com/canonical/gomock/gomock"
	"github.com/juju/clock"
	"github.com/juju/tc"

	"github.com/juju/juju/controller"
	coreapplication "github.com/juju/juju/core/application"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/core/instance"
	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/unit"
	"github.com/juju/juju/domain/controllernode"
	"github.com/juju/juju/domain/deployment/charm"
	machineerrors "github.com/juju/juju/domain/machine/errors"
	networkerrors "github.com/juju/juju/domain/network/errors"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/internal/cloudconfig"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/testing"
)

type recoveryBootstrapSuite struct {
	baseSuite

	controllerModel coremodel.Model
}

func TestRecoveryBootstrapSuite(t *stdtesting.T) {
	tc.Run(t, &recoveryBootstrapSuite{})
}

func (s *recoveryBootstrapSuite) SetUpTest(c *tc.C) {
	s.controllerModel = coremodel.Model{
		UUID:      tc.Must0(c, coremodel.NewUUID),
		ModelType: coremodel.IAAS,
	}
}

// TestRecoveryOperationSkipsIdentitySeeding proves that the recovery
// operation skips every identity seeding step — macaroon config, initial
// users, agent binary, storage pools, spaces reload, controller charm
// and authorized keys — because the archived databases provide them. The
// skipped services carry no mock expectations, so any call fails the
// test.
func (s *recoveryBootstrapSuite) TestRecoveryOperationSkipsIdentitySeeding(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.ensureBootstrapParamsWithRecovery(c, "/var/lib/juju/recovery/archive.tar.gz")

	s.controllerConfigService.EXPECT().ControllerConfig(gomock.Any()).Return(controller.Config{
		controller.ControllerUUIDKey:   "test-uuid",
		controller.JujuManagementSpace: "mgmt-space",
	}, nil)

	// Both password bridges run: the archived controller application
	// and unit keep the source's credentials, bridged to the passwords
	// bootstrap generated for the replacement.
	applicationUUID := coreapplication.UUID("controller-application-uuid")
	s.applicationService.EXPECT().GetApplicationUUIDByName(gomock.Any(), "controller").Return(applicationUUID, nil)
	s.agentPasswordService.EXPECT().SetApplicationPassword(gomock.Any(), applicationUUID, "application-password")
	s.agentPasswordService.EXPECT().SetUnitPassword(gomock.Any(), unit.Name("controller/0"), "unit-password")

	finalizerCalled := false
	finalizer := func(
		_ context.Context, _ AgentPasswordService, _ MachineService,
		params instancecfg.StateInitializationParams, agentPassword string,
	) error {
		finalizerCalled = true
		c.Check(params.BootstrapMachineInstanceId, tc.Equals, instance.Id("i-deadbeef"))
		c.Check(agentPassword, tc.Equals, "agent-password")
		// The recovery stage already patched the archived machine's
		// cloud instance onto the replacement; the finalizer's insert
		// is redundant and must be tolerated.
		return machineerrors.MachineCloudInstanceAlreadyExists
	}

	var removedKeys []string
	removeKeys := func(keys []string) error {
		removedKeys = keys
		return nil
	}

	spaceName := network.SpaceName("mgmt-space")
	s.networkService.EXPECT().GetAllSpaces(gomock.Any()).Return(nil, nil)
	s.networkService.EXPECT().SpaceByName(gomock.Any(), spaceName).Return(nil, networkerrors.SpaceNotFound)
	s.controllerNodeService.EXPECT().SetAPIAddresses(gomock.Any(), controllernode.SetAPIAddressArgs{
		APIPort: 42,
		Addresses: map[string]controllernode.APIAddressSet{
			"0": {},
		},
	})

	op, err := NewRecoveryBootstrap(RecoveryBootstrapConfig{
		ControllerConfigService: s.controllerConfigService,
		ControllerNodeService:   s.controllerNodeService,
		AgentPasswordService:    s.agentPasswordService,
		ApplicationService:      s.applicationService,
		MachineService:          s.machineService,
		NetworkService:          s.networkService,
		ControllerModel:         s.controllerModel,
		BootstrapAddressFinder: func(context.Context, instance.Id) (network.ProviderAddresses, error) {
			return nil, nil
		},
		DataDir:                s.dataDir,
		APIPort:                42,
		AgentFinalizer:         finalizer,
		AgentPassword:          "agent-password",
		ApplicationPassword:    "application-password",
		UnitPassword:           "unit-password",
		RemoveBootstrapSSHKeys: removeKeys,
		Logger:                 s.logger,
		Clock:                  clock.WallClock,
	})
	c.Assert(err, tc.ErrorIsNil)

	cleanup, err := op(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(cleanup, tc.IsNil)
	c.Check(finalizerCalled, tc.IsTrue)
	c.Check(removedKeys, tc.DeepEquals, []string{"bootstrap-ssh-key"})
}

// TestRecoveryOperationFinalizerError proves that a finalizer failure
// other than the already-recovered cloud instance fails the operation.
func (s *recoveryBootstrapSuite) TestRecoveryOperationFinalizerError(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.ensureBootstrapParamsWithRecovery(c, "/var/lib/juju/recovery/archive.tar.gz")

	s.controllerConfigService.EXPECT().ControllerConfig(gomock.Any()).Return(controller.Config{
		controller.ControllerUUIDKey:   "test-uuid",
		controller.JujuManagementSpace: "mgmt-space",
	}, nil)

	applicationUUID := coreapplication.UUID("controller-application-uuid")
	s.applicationService.EXPECT().GetApplicationUUIDByName(gomock.Any(), "controller").Return(applicationUUID, nil)
	s.agentPasswordService.EXPECT().SetApplicationPassword(gomock.Any(), applicationUUID, "application-password")
	s.agentPasswordService.EXPECT().SetUnitPassword(gomock.Any(), unit.Name("controller/0"), "unit-password")

	finalizer := func(
		context.Context, AgentPasswordService, MachineService,
		instancecfg.StateInitializationParams, string,
	) error {
		return errors.New("finalizer exploded")
	}

	op, err := NewRecoveryBootstrap(RecoveryBootstrapConfig{
		ControllerConfigService: s.controllerConfigService,
		ControllerNodeService:   s.controllerNodeService,
		AgentPasswordService:    s.agentPasswordService,
		ApplicationService:      s.applicationService,
		MachineService:          s.machineService,
		NetworkService:          s.networkService,
		ControllerModel:         s.controllerModel,
		BootstrapAddressFinder: func(context.Context, instance.Id) (network.ProviderAddresses, error) {
			return nil, nil
		},
		DataDir:                s.dataDir,
		APIPort:                42,
		AgentFinalizer:         finalizer,
		AgentPassword:          "agent-password",
		ApplicationPassword:    "application-password",
		UnitPassword:           "unit-password",
		RemoveBootstrapSSHKeys: func([]string) error { return nil },
		Logger:                 s.logger,
		Clock:                  clock.WallClock,
	})
	c.Assert(err, tc.ErrorIsNil)

	_, err = op(c.Context())
	c.Check(err, tc.ErrorMatches, "finalising agent: finalizer exploded")
}

// TestRecoveryOperationRequiresRecoveryArchive proves that selecting the
// recovery operation without a recovery archive in the bootstrap params
// is a mis-selection error, not a silent fresh bootstrap.
func (s *recoveryBootstrapSuite) TestRecoveryOperationRequiresRecoveryArchive(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.ensureBootstrapParamsWithRecovery(c, "")

	op, err := NewRecoveryBootstrap(RecoveryBootstrapConfig{
		ControllerConfigService: s.controllerConfigService,
		ControllerNodeService:   s.controllerNodeService,
		AgentPasswordService:    s.agentPasswordService,
		ApplicationService:      s.applicationService,
		MachineService:          s.machineService,
		NetworkService:          s.networkService,
		ControllerModel:         s.controllerModel,
		BootstrapAddressFinder: func(context.Context, instance.Id) (network.ProviderAddresses, error) {
			return nil, nil
		},
		DataDir: s.dataDir,
		APIPort: 42,
		AgentFinalizer: func(context.Context, AgentPasswordService, MachineService, instancecfg.StateInitializationParams, string) error {
			return nil
		},
		AgentPassword:          "agent-password",
		RemoveBootstrapSSHKeys: func([]string) error { return nil },
		Logger:                 s.logger,
		Clock:                  clock.WallClock,
	})
	c.Assert(err, tc.ErrorIsNil)

	_, err = op(c.Context())
	c.Check(err, tc.ErrorMatches, "recovery bootstrap selected without a recovery archive")
}

// ensureBootstrapParamsWithRecovery writes bootstrap params carrying the
// given recovery archive path into the test data dir.
func (s *recoveryBootstrapSuite) ensureBootstrapParamsWithRecovery(c *tc.C, recoveryArchivePath string) {
	cfg, err := config.New(config.NoDefaults, testing.FakeConfig())
	c.Assert(err, tc.ErrorIsNil)

	args := instancecfg.StateInitializationParams{
		BootstrapSSHAuthorizedKeys:  []string{"bootstrap-ssh-key"},
		ControllerModelConfig:       cfg,
		BootstrapMachineConstraints: constraints.MustParse("mem=1G"),
		BootstrapMachineInstanceId:  instance.Id("i-deadbeef"),
		ControllerCharmPath:         "obscura",
		ControllerCharmChannel:      charm.MakePermissiveChannel("", "stable", ""),
		RecoveryArchivePath:         recoveryArchivePath,
	}
	bytes, err := args.Marshal()
	c.Assert(err, tc.ErrorIsNil)

	err = os.WriteFile(filepath.Join(s.dataDir, cloudconfig.FileNameBootstrapParams), bytes, 0644)
	c.Assert(err, tc.ErrorIsNil)
}
