// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"

	"github.com/juju/juju/core/model"
)

// State defines an interface for interacting with the underlying state.
type State interface {
	Controller(ctx context.Context) (string, model.UUID, error)
}

// Service defines a service for interacting with the underlying state.
type Service struct {
	st State
}

// NewService returns a new Service for interacting with the underlying state.
func NewService(st State) *Service {
	return &Service{
		st: st,
	}
}

// Controller returns the controller UUID and the controller model UUID.
func (s *Service) Controller(ctx context.Context) (string, model.UUID, error) {
	return s.st.Controller(ctx)
}
