// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agent

import (
	"context"
	"time"

	"github.com/juju/gnuflag"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/agent/agentrecovery"
	agentconfig "github.com/juju/juju/agent/config"
	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/internal/agent/agentconf"
	"github.com/juju/juju/core/version"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/cloudspec"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/recovery"
)

// RecoveryCommand imports a backup before normal workers own the databases.
type RecoveryCommand struct {
	cmd.CommandBase
	agentconf.AgentConf
	Timeout time.Duration
}

func NewRecoveryCommand() *RecoveryCommand {
	return &RecoveryCommand{AgentConf: agentconf.NewAgentConf("")}
}

func (c *RecoveryCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{Name: "recovery-state", Purpose: "initialise recovered controller state"})
}

func (c *RecoveryCommand) SetFlags(f *gnuflag.FlagSet) {
	c.AgentConf.AddFlags(f)
	f.DurationVar(&c.Timeout, "timeout", 0, "set the recovery timeout")
}

func (c *RecoveryCommand) Init(args []string) error {
	if err := cmd.CheckEmpty(args); err != nil {
		return err
	}
	return c.AgentConf.CheckArgs(args)
}

func (c *RecoveryCommand) Run(ctx *cmd.Context) error {
	isRecovery, err := recovery.IsRecovery(c.DataDir())
	if err != nil {
		return errors.Capture(err)
	}
	if !isRecovery {
		return errors.New("recovery-state requires the provisioned recovery mode file")
	}
	params, err := recovery.ReadParams(c.DataDir())
	if err != nil {
		return errors.Capture(err)
	}
	if done, err := recovery.HasMarker(c.DataDir(), recovery.InitialisedFile, params.SHA256); err != nil {
		return errors.Capture(err)
	} else if done {
		return nil
	}
	if started, err := recovery.HasMarker(c.DataDir(), recovery.StartedFile, params.SHA256); err != nil {
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
	info, err := recovery.ValidateArchive(stdCtx, params.ArchivePath, params.SHA256)
	if err != nil {
		return errors.Capture(err)
	}
	if err := info.CheckAgentVersion(version.Current); err != nil {
		return errors.Capture(err)
	}
	if info.AgentVersion != params.AgentVersion || info.MachineName != params.MachineName {
		return errors.New("recovery parameters disagree with archive metadata")
	}
	if err := agentconfig.ReadAgentConfig(c, params.MachineName); err != nil {
		return errors.Errorf("reading recovery agent config: %w", err)
	}
	cloud, err := cloudspec.MakeCloudSpec(params.Node.ControllerCloud,
		params.Node.ControllerCloudRegion, params.Node.ControllerCloudCredential)
	if err != nil {
		return errors.Capture(err)
	}
	cloud.IsControllerCloud = true
	env, err := environs.New(stdCtx, environs.OpenParams{
		ControllerUUID: info.ControllerUUID, Cloud: cloud,
		Config: params.Node.ControllerModelConfig,
	}, environs.NoopCredentialInvalidator())
	if err != nil {
		return errors.Capture(err)
	}
	finder, err := environs.NewBootstrapAddressFinder(env, params.Node.BootstrapMachineInstanceId)
	if err != nil {
		return errors.Capture(err)
	}
	addresses, err := finder.BootstrapControllerAddresses(stdCtx)
	if err != nil {
		return errors.Capture(err)
	}
	// A failed multi-database import is not safely replayable. Persist intent
	// before the first database write and fail closed on a later invocation.
	if err := recovery.BeginInitialisation(c.DataDir(), params.SHA256); err != nil {
		return err
	}
	if err := c.ChangeConfig(func(cfg agent.ConfigSetter) error {
		return agentrecovery.Initialise(stdCtx, cfg, params, addresses, logger)
	}); err != nil {
		return errors.Capture(err)
	}
	if err := agent.WriteSystemIdentityFile(c.CurrentConfig()); err != nil {
		return errors.Capture(err)
	}
	return recovery.WriteMarker(c.DataDir(), recovery.InitialisedFile, params.SHA256)
}
