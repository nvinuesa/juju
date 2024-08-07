// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package charmrevisionupdater

import (
	"context"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/rpc/params"
)

// Option is a function that can be used to configure a Client.
type Option = base.Option

// WithTracer returns an Option that configures the Client to use the
// supplied tracer.
var WithTracer = base.WithTracer

// Client provides access to a worker's view of the state.
type Client struct {
	facade base.FacadeCaller
}

// NewClient returns a version of the state that provides functionality required by the worker.
func NewClient(caller base.APICaller, options ...Option) *Client {
	return &Client{base.NewFacadeCaller(caller, "CharmRevisionUpdater", options...)}
}

// UpdateLatestRevisions retrieves charm revision info from a repository
// and updates the revision info in state.
func (st *Client) UpdateLatestRevisions(ctx context.Context) error {
	result := new(params.ErrorResult)
	err := st.facade.FacadeCall(ctx, "UpdateLatestRevisions", nil, result)
	if err != nil {
		return err
	}
	if result.Error != nil {
		return result.Error
	}
	return nil
}
