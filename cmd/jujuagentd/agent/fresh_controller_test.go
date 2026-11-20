// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/juju/names/v6"
	"github.com/juju/tc"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/agent/agentbootstrap"
	"github.com/juju/juju/caas"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/cmd/internal/agent/agentconf"
	"github.com/juju/juju/controller"
	corelogger "github.com/juju/juju/core/logger"
	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/network"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/internal/cloudconfig"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/database"
	"github.com/juju/juju/internal/errors"
	k8sconstants "github.com/juju/juju/internal/provider/kubernetes/constants"
	"github.com/juju/juju/internal/testhelpers"
	jujutesting "github.com/juju/juju/internal/testing"
)

type freshControllerSuite struct {
	testhelpers.IsolationSuite
}

func TestFreshControllerSuite(t *testing.T) {
	tc.Run(t, &freshControllerSuite{})
}

func (s *freshControllerSuite) TestFreshIAASController(c *tc.C) {
	s.assertFreshController(c, false, nil)
}

func (s *freshControllerSuite) TestFreshK8sController(c *tc.C) {
	s.assertFreshController(c, true, nil)
}

func (s *freshControllerSuite) TestDatabaseFailureDoesNotPersistNewPassword(c *tc.C) {
	s.assertFreshController(c, false, errors.New("database failed"))
}

func (s *freshControllerSuite) TestRecoverySkipsDqliteInitialisation(c *tc.C) {
	s.assertRecoveryController(c, false, agent.BootstrapControllerId)
}

// TestRecoveryUsesArchivedMachineId checks that recovery from an archive
// written by a controller machine other than machine 0 reads the agent
// configuration for the archived logical machine identity, while still
// skipping BootstrapAgent and Dqlite initialisation.
func (s *freshControllerSuite) TestRecoveryUsesArchivedMachineId(c *tc.C) {
	s.assertRecoveryController(c, false, "7")
}

// TestRecoveryK8sController checks that the Kubernetes recovery workflow
// builds agent.conf from its template and reads the controller-0 agent
// configuration, while still skipping BootstrapAgent and Dqlite
// initialisation.
func (s *freshControllerSuite) TestRecoveryK8sController(c *tc.C) {
	s.assertRecoveryController(c, true, agent.BootstrapControllerId)
}

// controllerRunSpec holds the mode-dependent inputs for the shared
// fresh-bootstrap and recovery controller assertions.
type controllerRunSpec struct {
	isK8s     bool
	recovery  bool
	machineId string
	// databaseErr is only meaningful for the fresh-bootstrap workflow;
	// recovery never initialises a database.
	databaseErr error
}

func (s *freshControllerSuite) assertFreshController(c *tc.C, isK8s bool, databaseErr error) {
	s.assertControllerRun(c, controllerRunSpec{isK8s: isK8s, databaseErr: databaseErr})
}

// assertRecoveryController checks the recovery workflow against the given
// archived logical machine identity.
func (s *freshControllerSuite) assertRecoveryController(c *tc.C, isK8s bool, machineId string) {
	s.assertControllerRun(c, controllerRunSpec{isK8s: isK8s, recovery: true, machineId: machineId})
}

func (s *freshControllerSuite) assertControllerRun(c *tc.C, spec controllerRunSpec) {
	isK8s := spec.isK8s
	skipDqlite := spec.recovery
	databaseErr := spec.databaseErr
	machineId := spec.machineId
	if machineId == "" {
		machineId = agent.BootstrapControllerId
	}
	dataDir := c.MkDir()
	modelUUID := tc.Must0(c, coremodel.NewUUID)
	cloudType := "dummy"
	tag := names.Tag(names.NewMachineTag(machineId))
	if isK8s {
		cloudType = cloud.CloudTypeKubernetes
		tag = names.NewControllerAgentTag(agent.BootstrapControllerId)
	}
	modelConfig := jujutesting.CustomModelConfig(c, jujutesting.Attrs{
		"name": "controller", "uuid": modelUUID.String(), "type": cloudType,
		"agent-version": jujuversion.Current.String(),
	})
	params := instancecfg.StateInitializationParams{
		RecoveryMode:          skipDqlite,
		ControllerConfig:      jujutesting.FakeControllerConfig(),
		ControllerModelConfig: modelConfig,
		ControllerCloud: cloud.Cloud{
			Name: "test-cloud", Type: cloudType, AuthTypes: cloud.AuthTypes{cloud.EmptyAuthType},
		},
	}
	paramsData, err := params.Marshal()
	c.Assert(err, tc.ErrorIsNil)
	err = os.WriteFile(filepath.Join(dataDir, cloudconfig.FileNameBootstrapParams), paramsData, 0600)
	c.Assert(err, tc.ErrorIsNil)

	cfg, err := agent.NewStateMachineConfig(agent.AgentConfigParams{
		Paths: agent.Paths{DataDir: dataDir}, Tag: tag,
		UpgradedToVersion: jujuversion.Current, Password: "bootstrap-secret",
		Controller: jujutesting.ControllerTag, Model: names.NewModelTag(modelUUID.String()),
		APIAddresses: []string{"127.0.0.1:17070"}, CACert: jujutesting.CACert,
	}, controller.ControllerAgentInfo{
		Cert: jujutesting.ServerCert, PrivateKey: jujutesting.ServerKey,
		CAPrivateKey: jujutesting.CAKey, APIPort: 17070, SystemIdentity: "provided-system-identity",
	})
	c.Assert(err, tc.ErrorIsNil)
	cfg.SetPassword("agent-before")
	if isK8s {
		// The fresh Kubernetes workflow must create agent.conf from its template.
		template := filepath.Join(agent.Dir(dataDir, tag), k8sconstants.TemplateFileNameAgentConf)
		err = os.MkdirAll(filepath.Dir(template), 0700)
		c.Assert(err, tc.ErrorIsNil)
		data, err := cfg.Render()
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(os.WriteFile(template, data, 0600), tc.ErrorIsNil)
	} else {
		c.Assert(cfg.Write(), tc.ErrorIsNil)
	}

	addresses := network.NewMachineAddresses([]string{"10.0.0.1"}).AsProviderAddresses()
	checkOpen := func(args environs.OpenParams) {
		c.Check(args.Cloud.IsControllerCloud, tc.IsTrue)
		c.Check(args.ControllerUUID, tc.Equals, jujutesting.ControllerTag.Id())
	}
	s.PatchValue(&environsNewIAAS, func(_ context.Context, args environs.OpenParams, _ environs.CredentialInvalidator) (environs.Environ, error) {
		c.Check(isK8s, tc.IsFalse)
		checkOpen(args)
		return &freshBootstrapEnviron{cfg: modelConfig, addresses: addresses}, nil
	})
	s.PatchValue(&environsNewK8s, func(_ context.Context, args environs.OpenParams, _ environs.CredentialInvalidator) (caas.Broker, error) {
		c.Check(isK8s, tc.IsTrue)
		checkOpen(args)
		return &freshBootstrapBroker{cfg: modelConfig, addresses: addresses}, nil
	})
	s.PatchValue(&sshGenerateKey, func(string) (string, string, error) {
		c.Check(isK8s, tc.IsFalse)
		return "fresh-system-identity", "fresh-public-key", nil
	})
	expectedIdentity := "fresh-system-identity"
	if isK8s {
		expectedIdentity = "provided-system-identity"
	}

	bootstrapAgentCalled := false
	databaseCalled := false
	if skipDqlite {
		command := NewRecoverCommand()
		command.AgentConf = agentconf.NewAgentConf(dataDir)
		command.MachineID = machineId
		err = command.Run(cmdtesting.Context(c))
		c.Assert(err, tc.ErrorIsNil)
	} else {
		command := NewBootstrapCommand()
		command.AgentConf = agentconf.NewAgentConf(dataDir)
		const bindAddress = "192.168.1.10"
		command.DqliteBindAddressResolver = func(
			_ context.Context, gotAddresses network.ProviderAddresses,
		) (string, error) {
			c.Check(gotAddresses, tc.DeepEquals, addresses)
			return bindAddress, nil
		}
		command.BootstrapAgent = func(args agentbootstrap.AgentBootstrapArgs) (*agentbootstrap.AgentBootstrap, error) {
			bootstrapAgentCalled = true
			c.Check(args.StateInitialisationParams.SSHServerHostKey, tc.Not(tc.Equals), "")
			c.Check(args.BootstrapMachineAddresses, tc.DeepEquals, addresses)
			return agentbootstrap.NewAgentBootstrap(args)
		}
		command.DqliteInitialiser = func(_ context.Context, _ database.BootstrapNodeManager, gotBindAddress string, gotModelUUID coremodel.UUID, _ corelogger.Logger, _ ...database.BootstrapOpt) error {
			databaseCalled = true
			c.Check(gotBindAddress, tc.Equals, bindAddress)
			c.Check(gotModelUUID, tc.Equals, modelUUID)
			// Identity is persisted before database work; the new password is not.
			identity, err := os.ReadFile(cfg.SystemIdentityPath())
			c.Assert(err, tc.ErrorIsNil)
			c.Check(string(identity), tc.Equals, expectedIdentity)
			stored, err := agent.ReadConfig(agent.ConfigPath(dataDir, tag))
			c.Assert(err, tc.ErrorIsNil)
			info, ok := stored.ControllerAgentInfo()
			c.Assert(ok, tc.IsTrue)
			c.Check(info.SystemIdentity, tc.Equals, expectedIdentity)
			apiInfo, ok := stored.APIInfo()
			c.Assert(ok, tc.IsTrue)
			c.Check(apiInfo.Password, tc.Equals, "agent-before")
			return databaseErr
		}
		err = command.Run(cmdtesting.Context(c))
		if databaseErr == nil {
			c.Assert(err, tc.ErrorIsNil)
		} else {
			c.Check(err, tc.ErrorIs, databaseErr)
		}
	}
	if skipDqlite {
		c.Check(databaseCalled, tc.IsFalse)
		c.Check(bootstrapAgentCalled, tc.IsFalse)
		identity, err := os.ReadFile(cfg.SystemIdentityPath())
		c.Assert(err, tc.ErrorIsNil)
		c.Check(string(identity), tc.Equals, expectedIdentity)
	} else {
		c.Check(databaseCalled, tc.IsTrue)
		c.Check(bootstrapAgentCalled, tc.IsTrue)
	}
	stored, err := agent.ReadConfig(agent.ConfigPath(dataDir, tag))
	c.Assert(err, tc.ErrorIsNil)
	apiInfo, ok := stored.APIInfo()
	c.Assert(ok, tc.IsTrue)
	if databaseErr == nil && !skipDqlite {
		c.Check(apiInfo.Password, tc.Not(tc.Equals), "agent-before")
		c.Check(apiInfo.Password, tc.Not(tc.Equals), "")
	} else {
		c.Check(apiInfo.Password, tc.Equals, "agent-before")
	}
}

type freshBootstrapEnviron struct {
	environs.Environ
	cfg       *config.Config
	addresses network.ProviderAddresses
}

func (e *freshBootstrapEnviron) Config() *config.Config { return e.cfg }
func (e *freshBootstrapEnviron) BootstrapControllerAddresses(context.Context) (network.ProviderAddresses, error) {
	return e.addresses, nil
}

type freshBootstrapBroker struct {
	caas.Broker
	cfg       *config.Config
	addresses network.ProviderAddresses
}

func (e *freshBootstrapBroker) Config() *config.Config { return e.cfg }
func (e *freshBootstrapBroker) BootstrapControllerAddresses(context.Context) (network.ProviderAddresses, error) {
	return e.addresses, nil
}
