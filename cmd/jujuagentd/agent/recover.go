// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agent

import (
	"context"
	"time"

	"github.com/juju/gnuflag"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/agent/agentbootstrap"
	"github.com/juju/juju/agent/agentrecovery"
	agentconfig "github.com/juju/juju/agent/config"
	"github.com/juju/juju/cloud"
	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/internal/agent/agentconf"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/environs"
	environscloudspec "github.com/juju/juju/environs/cloudspec"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/password"
	internalrecovery "github.com/juju/juju/internal/recovery"
)

// RecoverCommand represents a jujuagentd recover-state command. It prepares a
// replacement controller's local configuration from an archived controller
// and replays its databases. It is invoked instead of bootstrap-state by the
// recovery setup generated for a replacement instance, and never performs a
// fresh bootstrap.
type RecoverCommand struct {
	cmd.CommandBase
	agentconf.AgentConf
	Timeout                   time.Duration
	DqliteBindAddressResolver agentbootstrap.DqliteBindAddressResolverFunc
}

// NewRecoverCommand returns a new RecoverCommand that has been initialised.
func NewRecoverCommand() *RecoverCommand {
	return &RecoverCommand{
		AgentConf:                 agentconf.NewAgentConf(""),
		DqliteBindAddressResolver: agentbootstrap.NewDqliteBindAddressResolver(logger),
	}
}

// Info returns a description of the command.
func (c *RecoverCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name:    "recover-state",
		Purpose: "initialise juju state for controller recovery",
	})
}

// SetFlags adds the flags for this command to the passed gnuflag.FlagSet.
func (c *RecoverCommand) SetFlags(f *gnuflag.FlagSet) {
	c.AgentConf.AddFlags(f)
	f.DurationVar(&c.Timeout, "timeout", time.Duration(0), "set the recovery timeout")
}

// Init initialises the command for running.
func (c *RecoverCommand) Init(args []string) error {
	if err := cmd.CheckEmpty(args); err != nil {
		return err
	}
	return c.AgentConf.CheckArgs(args)
}

// Run initialises state for a recovered controller.
func (c *RecoverCommand) Run(ctx *cmd.Context) error {
	isRecovery, err := internalrecovery.IsRecovery(c.DataDir())
	if err != nil {
		return errors.Capture(err)
	}
	if !isRecovery {
		return errors.New("recover-state requires provisioned recovery parameters")
	}
	params, err := internalrecovery.ReadParams(c.DataDir())
	if err != nil {
		return errors.Capture(err)
	}
	if done, err := internalrecovery.HasMarker(
		c.DataDir(), internalrecovery.InitialisedFile, params.SHA256,
	); err != nil {
		return errors.Capture(err)
	} else if done {
		// Kubernetes restarts the pod after the import; the marker makes
		// the command idempotent.
		return nil
	}
	return errors.Capture(c.initialiseRecoveryDatabase(ctx, params))
}

// initialiseRecoveryDatabase validates the uploaded archive and initialises
// the replacement Dqlite controller database with its controller node row.
func (c *RecoverCommand) initialiseRecoveryDatabase(ctx *cmd.Context, params internalrecovery.Params) error {
	if started, err := internalrecovery.HasMarker(
		c.DataDir(), internalrecovery.StartedFile, params.SHA256,
	); err != nil {
		return errors.Capture(err)
	} else if started {
		return errors.New("previous recovery import did not complete; provision a new replacement")
	}

	stdCtx := ctx.Context
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		stdCtx, cancel = context.WithTimeout(stdCtx, c.Timeout)
		defer cancel()
	}

	info, err := internalrecovery.ValidateArchive(stdCtx, logger, params.ArchivePath, params.SHA256)
	if err != nil {
		return errors.Capture(err)
	}
	if err := info.CheckAgentVersion(jujuversion.Current); err != nil {
		return errors.Capture(err)
	}

	node := params.Node
	isK8s := node.ControllerCloud.Type == cloud.CloudTypeKubernetes
	if isK8s {
		if err := ensureConfigFilesForK8s(c.DataDir()); err != nil {
			return errors.Capture(err)
		}
	}
	if err := agentconfig.ReadAgentConfig(c, params.MachineName); err != nil {
		return errors.Errorf("reading recovery agent config: %w", err)
	}
	if err := agent.WriteSystemIdentityFile(c.CurrentConfig()); err != nil {
		return errors.Capture(err)
	}

	cloudSpec, err := environscloudspec.MakeCloudSpec(
		node.ControllerCloud, node.ControllerCloudRegion, node.ControllerCloudCredential,
	)
	if err != nil {
		return errors.Capture(err)
	}
	cloudSpec.IsControllerCloud = true
	openParams := environs.OpenParams{
		ControllerUUID: node.ControllerConfig.ControllerUUID(),
		Cloud:          cloudSpec,
		Config:         node.ControllerModelConfig,
	}
	var env environs.BootstrapEnviron
	if isK8s {
		env, err = environsNewK8s(stdCtx, openParams, environs.NoopCredentialInvalidator())
	} else {
		env, err = environsNewIAAS(stdCtx, openParams, environs.NoopCredentialInvalidator())
	}
	if err != nil {
		return errors.Capture(err)
	}

	addresses, err := bootstrapControllerAddresses(stdCtx, env, node.BootstrapMachineInstanceId)
	if err != nil {
		return errors.Errorf("getting replacement controller addresses: %w", err)
	}
	bindAddress, err := c.DqliteBindAddressResolver(stdCtx, addresses)
	if err != nil {
		return errors.Capture(err)
	}

	if err := internalrecovery.BeginInitialisation(c.DataDir(), params.SHA256); err != nil {
		return errors.Capture(err)
	}

	if err := agentrecovery.Initialise(
		stdCtx, c.CurrentConfig(), params, addresses, bindAddress, logger,
	); err != nil {
		return errors.Capture(err)
	}

	newPassword, err := password.RandomPassword()
	if err != nil {
		return errors.Capture(err)
	}
	if err := c.ChangeConfig(func(agentConfig agent.ConfigSetter) error {
		agentConfig.SetPassword(newPassword)
		return nil
	}); err != nil {
		return errors.Errorf("cannot write agent config: %v", err)
	}
	if err := internalrecovery.WriteMarker(
		c.DataDir(), internalrecovery.InitialisedFile, params.SHA256,
	); err != nil {
		return errors.Capture(err)
	}

	logger.Infof(ctx, "recovery finalisation is not available in this build")
	return nil
}
