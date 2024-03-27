// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"

	"github.com/juju/juju/domain/model"
)

// ModelState is the model state required by this service. This is the model
// database state, not the controller state.
type ModelState interface {
	// Create creates a new model with all of its associated metadata.
	Create(context.Context, model.ReadOnlyModelCreationArgs) error
}

// ModelService defines a service for interacting with the underlying model
// state, as opposed to the controller state.
type ModelService struct {
	st ModelState
}

// NewModelService returns a new Service for interacting with a models state.
func NewModelService(st ModelState) *ModelService {
	return &ModelService{
		st: st,
	}
}

// CreateModel is responsible for creating a new model within the model
// database.
//
// The following error types can be expected to be returned:
// - [modelerrors.AlreadyExists]: When the model uuid is already in use.
func (s *ModelService) CreateModel(
	ctx context.Context,
	args model.ReadOnlyModelCreationArgs,
) error {
	if err := args.Validate(); err != nil {
		return err
	}

	return s.st.Create(ctx, args)
}