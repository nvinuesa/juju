// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package migrationstatus

import (
	"context"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v6"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/rpc/params"
)

// Option is a function that can be used to configure a Client.
type Option = base.Option

// WithTracer returns an Option that configures the Client to use the
// supplied tracer.
var WithTracer = base.WithTracer

// NewClient returns a new Client based on an existing API connection.
func NewClient(caller base.APICaller, options ...Option) *Client {
	return &Client{
		facade: base.NewFacadeCaller(caller, "MigrationStatus", options...),
	}
}

// Client is the client-side API for the MigrationStatus facade. It provides
// read-only, credential-free migration status information to CLI commands.
type Client struct {
	facade base.FacadeCaller
}

// MigrationStatus holds the credential-free status of a model migration as
// returned by the MigrationStatus facade. It does not include target
// controller addresses, CA certificates, passwords, macaroons, or tokens.
type MigrationStatus struct {
	// MigrationId is the unique identifier of the migration.
	MigrationId string

	// Phase is the current migration phase name.
	Phase string

	// PhaseChangedTime is when the migration entered the current phase.
	PhaseChangedTime time.Time

	// StartTime is when the migration was initiated.
	StartTime time.Time

	// StatusMessage is the last human-readable progress message set by
	// the migrationmaster worker.
	StatusMessage string

	// StatusMessageTime is when the status message was recorded.
	StatusMessageTime time.Time

	// TargetControllerUUID is the UUID of the target controller.
	TargetControllerUUID string

	// TargetControllerAlias is the alias of the target controller.
	TargetControllerAlias string
}

// MigrationStatus returns the current status of the latest migration for
// the specified model.
func (c *Client) MigrationStatus(ctx context.Context, modelUUID string) (MigrationStatus, error) {
	args := params.ModelArgs{
		ModelTag: names.NewModelTag(modelUUID).String(),
	}
	var result params.MigrationStatusResult
	err := c.facade.FacadeCall(ctx, "MigrationStatus", args, &result)
	if err != nil {
		return MigrationStatus{}, errors.Trace(err)
	}
	return MigrationStatus{
		MigrationId:           result.MigrationId,
		Phase:                 result.Phase,
		PhaseChangedTime:      result.PhaseChangedTime,
		StartTime:             result.StartTime,
		StatusMessage:         result.StatusMessage,
		StatusMessageTime:     result.StatusMessageTime,
		TargetControllerUUID:  result.TargetControllerUUID,
		TargetControllerAlias: result.TargetControllerAlias,
	}, nil
}
