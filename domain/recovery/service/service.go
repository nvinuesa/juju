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

	// GetControllerMachineName returns the first live machine hosting a live
	// controller unit, in name order.
	GetControllerMachineName(context.Context) (string, error)

	// PatchControllerMachine applies the replacement's physical facts.
	// Failure rolls back.
	PatchControllerMachine(context.Context, domainrecovery.MachinePatch) error

	// PatchControllerUnitAddresses targets the live controller unit on the
	// named machine, or the lowest-named live controller unit when the name
	// is empty.
	PatchControllerUnitAddresses(context.Context, string, domainrecovery.ControllerUnitAddressPatch) error

	// PatchControllerCredential rebinds archived authentication to the
	// replacement's.
	PatchControllerCredential(context.Context, string, string, map[string]string) error
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

// PatchControllerMachine applies the replacement's physical facts to the
// archived controller machine, returning the chosen machine name. A preset
// patch name is honoured; otherwise the live archived machine is resolved
// from the loaded dump.
func (s *Service) PatchControllerMachine(ctx context.Context, model coremodel.UUID, patch domainrecovery.MachinePatch) (string, error) {
	st, err := s.modelState(ctx, model)
	if err != nil {
		return "", errors.Capture(err)
	}
	name := patch.MachineName
	if name == "" {
		if name, err = st.GetControllerMachineName(ctx); err != nil {
			return "", errors.Capture(err)
		}
	}
	patch.MachineName = name
	if err := st.PatchControllerMachine(ctx, patch); err != nil {
		return "", errors.Errorf("patching controller machine %q: %w", name, err)
	}
	return name, nil
}

// PatchControllerUnitAddresses replaces the controller unit's IP and DNS
// observations with the replacement's addresses. machineName is the patch
// target PatchControllerMachine returned; it is empty where there is no
// machine (Kubernetes).
func (s *Service) PatchControllerUnitAddresses(ctx context.Context, model coremodel.UUID, machineName string, values []string) error {
	st, err := s.modelState(ctx, model)
	if err != nil {
		return errors.Capture(err)
	}
	patch, err := NewControllerUnitAddressPatch(values)
	if err != nil {
		return errors.Capture(err)
	}
	return errors.Capture(st.PatchControllerUnitAddresses(ctx, machineName, patch))
}

// PatchControllerCredential rebinds the controller model credential's
// authentication, retaining its archived identity.
func (s *Service) PatchControllerCredential(ctx context.Context, modelUUID, authType string, attributes map[string]string) error {
	return errors.Capture(s.controller.PatchControllerCredential(ctx, modelUUID, authType, attributes))
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
