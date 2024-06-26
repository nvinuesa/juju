// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"

	"github.com/juju/errors"

	"github.com/juju/juju/domain/life"
	"github.com/juju/juju/internal/uuid"
)

// State describes retrieval and persistence methods for machines.
type State interface {
	// CreateMachine persists the input machine entity.
	CreateMachine(context.Context, string, string, string) error

	// DeleteMachine deletes the input machine entity.
	DeleteMachine(context.Context, string) error

	// InitialWatchStatement returns the table and the initial watch statement
	// for the machines.
	InitialWatchStatement() (string, string)

	// GetMachineLife returns the life status of the specified machine.
	GetMachineLife(context.Context, string) (*life.Life, error)
}

// Service provides the API for working with machines.
type Service struct {
	st State
}

// NewService returns a new service reference wrapping the input state.
func NewService(st State) *Service {
	return &Service{
		st: st,
	}
}

// CreateMachine creates the specified machine.
func (s *Service) CreateMachine(ctx context.Context, machineId string) (string, error) {
	// Make a new UUIDs for the net-node and the machine.
	// We want to do this in the service layer so that if retries are invoked at
	// the state layer we don't keep regenerating.
	nodeUUID, err := uuid.NewUUID()
	if err != nil {
		return "", errors.Annotatef(err, "creating machine %q", machineId)
	}
	machineUUID, err := uuid.NewUUID()
	if err != nil {
		return "", errors.Annotatef(err, "creating machine %q", machineId)
	}

	err = s.st.CreateMachine(ctx, machineId, nodeUUID.String(), machineUUID.String())

	return machineUUID.String(), errors.Annotatef(err, "creating machine %q", machineId)
}

// DeleteMachine deletes the specified machine.
func (s *Service) DeleteMachine(ctx context.Context, machineId string) error {
	err := s.st.DeleteMachine(ctx, machineId)
	return errors.Annotatef(err, "deleting machine %q", machineId)
}

// GetLife returns the GetMachineLife status of the specified machine.
func (s *Service) GetMachineLife(ctx context.Context, machineId string) (*life.Life, error) {
	life, err := s.st.GetMachineLife(ctx, machineId)
	return life, errors.Annotatef(err, "getting life status for machine %q", machineId)
}
