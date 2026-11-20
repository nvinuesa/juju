// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agent

import (
	"context"
	"os"
	"path"
	"time"

	"github.com/juju/gnuflag"

	"github.com/juju/juju/agent"
	agentconfig "github.com/juju/juju/agent/config"
	"github.com/juju/juju/cloud"
	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/internal/agent/agentconf"
	"github.com/juju/juju/internal/cloudconfig"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/errors"
)

// RecoverCommand represents a jujud recover-state command. It prepares a
// replacement controller's local configuration from an archived controller
// and stops before Dqlite initialisation and state import. It is invoked
// instead of bootstrap-state by the recovery setup generated for a
// replacement instance, and never performs a fresh bootstrap.
type RecoverCommand struct {
	cmd.CommandBase
	agentconf.AgentConf
	// Timeout is the maximum duration to wait for recovery to complete.
	// A zero timeout means no deadline.
	Timeout time.Duration
	// MachineID is the archived logical machine identity whose agent
	// configuration is read for the replacement controller.
	MachineID string
}

// NewRecoverCommand returns a new RecoverCommand that has been initialised.
func NewRecoverCommand() *RecoverCommand {
	return &RecoverCommand{
		AgentConf: agentconf.NewAgentConf(""),
		MachineID: agent.BootstrapControllerId,
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
	f.StringVar(&c.MachineID, "machine-id", agent.BootstrapControllerId,
		"the archived logical machine identity whose agent configuration to read")
}

// Init initialises the command for running.
func (c *RecoverCommand) Init(args []string) error {
	if err := cmd.CheckEmpty(args); err != nil {
		return err
	}
	return c.AgentConf.CheckArgs(args)
}

// Run initialises state for a recovered environment.
func (c *RecoverCommand) Run(ctx *cmd.Context) error {
	bootstrapParamsData, err := os.ReadFile(path.Join(c.DataDir(), cloudconfig.FileNameBootstrapParams))
	if err != nil {
		return errors.Errorf("reading bootstrap params file: %w", err)
	}
	var args instancecfg.StateInitializationParams
	if err := args.Unmarshal(bootstrapParamsData); err != nil {
		return errors.Capture(err)
	}
	// The timeout bounds the recovery initialisation; a zero timeout means
	// no deadline. Later recovery stages inherit the deadline for their
	// blocking work (archive validation, database initialisation).
	recoveryCtx := ctx.Context
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		recoveryCtx, cancel = context.WithTimeout(recoveryCtx, c.Timeout)
		defer cancel()
	}
	return errors.Capture(c.initialiseRecoveryController(recoveryCtx, args))
}

// initialiseRecoveryController prepares a replacement controller's local
// configuration from the archived identity and stops before Dqlite
// initialisation and state import. Unlike a fresh bootstrap it does not
// contact the provider, look for newer agent binaries, or resolve bootstrap
// addresses: the replacement runs the exact archived agent version and keeps
// the archived logical machine identity, so surviving agents keep trusting
// the replacement controller.
func (c *RecoverCommand) initialiseRecoveryController(
	ctx context.Context, args instancecfg.StateInitializationParams,
) error {
	isK8s := args.ControllerCloud.Type == cloud.CloudTypeKubernetes
	if isK8s {
		if err := ensureConfigFilesForK8s(c.DataDir()); err != nil {
			return errors.Capture(err)
		}
	}

	// The replacement agent configuration is written for the archived logical
	// machine, which need not be the bootstrap controller machine.
	if err := agentconfig.ReadAgentConfig(c, c.MachineID); err != nil {
		return errors.Errorf("cannot read config: %w", err)
	}
	agentConfig := c.CurrentConfig()
	info, ok := agentConfig.ControllerAgentInfo()
	if !ok {
		return errors.Errorf("bootstrap machine config has no state serving info")
	}
	if err := ensureKeys(isK8s, &args, &info); err != nil {
		return errors.Capture(err)
	}
	if err := c.ChangeConfig(func(agentConfig agent.ConfigSetter) error {
		agentConfig.SetControllerAgentInfo(info)
		return nil
	}); err != nil {
		return errors.Errorf("cannot write agent config: %w", err)
	}

	agentConfig = c.CurrentConfig()

	// Create system-identity file
	if err := agent.WriteSystemIdentityFile(agentConfig); err != nil {
		return errors.Capture(err)
	}

	logger.Infof(ctx, "recovery database import is not available in this build")
	return nil
}
