// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"
	"sync"

	coremodel "github.com/juju/juju/core/model"
	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/internal/errors"
)

// State supplies recovery persistence for one database. Load methods
// atomically replace tables selected by service policy.
type State interface {
	LoadControllerDump(context.Context, *domainrecovery.ControllerDump, string) error
	LoadModelDump(context.Context, *domainrecovery.ModelDump, string) error
}

// Service coordinates persistence during exclusive recovery bootstrap. The
// database session owns its runners and must outlive this service. Loading is
// sequential: controller, then models in sorted order. A failure can leave
// earlier databases committed; discard the recovery target.
type Service struct {
	controller State
	openModel  func(context.Context, string) (State, error)
	nodeID     string
	mu         sync.Mutex
	models     map[string]State
}

// NewService requires both dependencies before recovery can begin. The node
// ID identifies the provisioned replacement's object store node; every
// placement rebuilt during the loads rebinds to it.
func NewService(controller State, openModel func(context.Context, string) (State, error), nodeID string) (*Service, error) {
	if controller == nil || openModel == nil {
		return nil, errors.New("recovery requires controller state and a model state factory")
	}
	return &Service{controller: controller, openModel: openModel, nodeID: nodeID, models: make(map[string]State)}, nil
}

func (s *Service) modelState(ctx context.Context, model coremodel.UUID) (State, error) {
	if err := model.Validate(); err != nil {
		return nil, errors.Capture(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Capture(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.models[model.String()]; ok {
		return st, nil
	}
	st, err := s.openModel(ctx, model.String())
	if err != nil {
		return nil, errors.Errorf("opening database for model %q: %w", model, err)
	}
	if st == nil {
		return nil, errors.Errorf("model %q state factory returned nil state", model)
	}
	s.models[model.String()] = st
	return st, nil
}
