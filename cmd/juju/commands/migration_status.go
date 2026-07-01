// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"context"
	"io"
	"time"

	"github.com/juju/errors"
	"github.com/juju/gnuflag"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/controller/migrationstatus"
	"github.com/juju/juju/api/jujuclient"
	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/modelcmd"
	coremigration "github.com/juju/juju/core/migration"
)

func newMigrationStatusCommand() modelcmd.ModelCommand {
	var cmd migrationStatusCommand
	cmd.newAPIRoot = cmd.CommandBase.NewAPIRoot
	return modelcmd.Wrap(&cmd,
		modelcmd.WrapSkipModelFlags,
	)
}

// migrationStatusCommand reports the status of a model migration.
type migrationStatusCommand struct {
	modelcmd.ModelCommandBase

	// Overridden by tests
	newAPIRoot   func(context.Context, jujuclient.ClientStore, string, string) (api.Connection, error)
	migStatusAPI migrationStatusAPI
}

type migrationStatusAPI interface {
	MigrationStatus(ctx context.Context, modelUUID string) (migrationstatus.MigrationStatus, error)
}

const migrationStatusDoc = `
The ` + "`juju migration-status`" + ` command reports the current status of the
most recent migration attempt for the specified model. It shows the migration
phase, a human-readable progress message, and the target controller.

This command is useful after running ` + "`juju migrate`" + ` to track the
progress of the migration without needing to consult the controller logs.

If no migration has been attempted for the model, the command reports that
no migration was found.

`

// Info implements cmd.Command.
func (c *migrationStatusCommand) Info() *cmd.Info {
	return jujucmd.Info(&cmd.Info{
		Name:    "migration-status",
		Args:    "<model-name>",
		Purpose: "Show the status of a model migration.",
		Doc:     migrationStatusDoc,
		SeeAlso: []string{
			"migrate",
			"status",
		},
	})
}

// SetFlags implements cmd.Command.
func (c *migrationStatusCommand) SetFlags(f *gnuflag.FlagSet) {
	c.ModelCommandBase.SetFlags(f)
}

// Init implements cmd.Command.
func (c *migrationStatusCommand) Init(args []string) error {
	if len(args) < 1 {
		return errors.New("model not specified")
	}
	if len(args) > 1 {
		return errors.New("too many arguments specified")
	}
	if err := c.SetModelIdentifier(args[0], false); err != nil {
		return errors.Trace(err)
	}
	return nil
}

// Run implements cmd.Command.
func (c *migrationStatusCommand) Run(ctx *cmd.Context) error {
	modelName, err := c.ModelIdentifier()
	if err != nil {
		return errors.Trace(err)
	}
	uuids, err := c.ModelUUIDs(ctx, []string{modelName})
	if err != nil {
		return errors.Trace(err)
	}
	modelUUID := uuids[0]

	controllerName, err := c.ControllerName()
	if err != nil {
		return err
	}

	api, closer, err := c.getMigrationStatusAPI(ctx, controllerName)
	if err != nil {
		return err
	}
	defer func() { _ = closer.Close() }()

	status, err := api.MigrationStatus(ctx, modelUUID)
	if err != nil {
		if errors.Is(err, errors.NotFound) {
			ctx.Infof("No migration found for model %q.", modelName)
			return nil
		}
		return err
	}

	c.formatStatus(ctx, modelName, status)
	return nil
}

func (c *migrationStatusCommand) getMigrationStatusAPI(
	ctx context.Context, controllerName string,
) (migrationStatusAPI, io.Closer, error) {
	if c.migStatusAPI != nil {
		return c.migStatusAPI, nopCloser{}, nil
	}
	apiRoot, err := c.newAPIRoot(ctx, c.ClientStore(), controllerName, "")
	if err != nil {
		return nil, nil, errors.Trace(err)
	}
	return migrationstatus.NewClient(apiRoot), apiRoot, nil
}

func (c *migrationStatusCommand) formatStatus(ctx *cmd.Context, modelName string, status migrationstatus.MigrationStatus) {
	ctx.Infof("Migration status for model %q", modelName)
	ctx.Infof("")
	ctx.Infof("  Migration ID:       %s", status.MigrationId)
	phase, _ := coremigration.ParsePhase(status.Phase)
	ctx.Infof("  Status:             %s (%s)", phaseDescription(phase), status.Phase)
	ctx.Infof("  Started:            %s", formatTime(status.StartTime))
	ctx.Infof("  Phase changed:      %s", formatTime(status.PhaseChangedTime))
	if status.StatusMessage != "" {
		ctx.Infof("  Last message:       %s", status.StatusMessage)
	}
	if status.TargetControllerAlias != "" {
		ctx.Infof("  Target controller:  %s", status.TargetControllerAlias)
	} else if status.TargetControllerUUID != "" {
		ctx.Infof("  Target controller:  %s", status.TargetControllerUUID)
	}
}

// phaseDescription maps a migration phase to a user-friendly description.
func phaseDescription(phase coremigration.Phase) string {
	switch phase {
	case coremigration.QUIESCE:
		return "Preparing model for migration"
	case coremigration.IMPORT:
		return "Transferring model data to target controller"
	case coremigration.VALIDATION:
		return "Validating migrated model on target controller"
	case coremigration.SUCCESS:
		return "Migration successful, finalizing"
	case coremigration.LOGTRANSFER:
		return "Transferring logs to target controller"
	case coremigration.REAP:
		return "Cleaning up model on source controller"
	case coremigration.REAPFAILED:
		return "Cleanup failed - manual intervention required"
	case coremigration.DONE:
		return "Migration complete"
	case coremigration.ABORT:
		return "Migration aborting"
	case coremigration.ABORTDONE:
		return "Migration aborted - model returned to source controller"
	default:
		return "Unknown"
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Format("2006-01-02 15:04:05 MST")
}

// nopCloser is a no-op Closer for the test-injected API path.
type nopCloser struct{}

func (nopCloser) Close() error { return nil }
