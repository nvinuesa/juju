// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"

	"github.com/juju/errors"

	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/domain/life"
	"github.com/juju/juju/internal/uuid"
)

// State describes retrieval and persistence methods for machines.
type State interface {
	// CreateMachine persists the input machine entity.
	CreateMachine(context.Context, machine.Name, string, string) error

	// DeleteMachine deletes the input machine entity.
	DeleteMachine(context.Context, machine.Name) error

	// InitialWatchStatement returns the table and the initial watch statement
	// for the machines.
	InitialWatchStatement() (string, string)

	// GetMachineLife returns the life status of the specified machine.
	// It returns a NotFound if the given machine doesn't exist.
	GetMachineLife(context.Context, machine.Name) (*life.Life, error)

	// SetMachineLife sets the life status of the specified machine.
	// It returns a NotFound if the provided machine doesn't exist.
	SetMachineLife(context.Context, machine.Name, life.Life) error

	// AllMachineNames retrieves the names of all machines in the model.
	// If there's no machine, it returns an empty slice.
	AllMachineNames(context.Context) ([]machine.Name, error)

	// InitialWatchInstanceStatement returns the table and the initial watch statement
	// for the machine cloud instances.
	InitialWatchInstanceStatement() (string, string)

	// InstanceId returns the cloud specific instance id for this machine.
	// If the machine is not provisioned, it returns a NotProvisionedError.
	InstanceId(context.Context, machine.Name) (string, error)

	// InstanceStatus returns the cloud specific instance status for this
	// machine.
	// If the machine is not provisioned, it returns a NotProvisionedError.
	InstanceStatus(context.Context, machine.Name) (string, error)

	// HardwareCharacteristics returns the hardware characteristics struct with
	// data retrieved from the machine cloud instance table.
	HardwareCharacteristics(context.Context, string) (*instance.HardwareCharacteristics, error)

	// SetMachineCloudInstance sets an entry in the machine cloud instance table
	// along with the instance tags and the link to a lxd profile if any.
	SetMachineCloudInstance(context.Context, string, instance.Id, instance.HardwareCharacteristics) error

	// DeleteMachineCloudInstance removes an entry in the machine cloud instance
	// table along with the instance tags and the link to a lxd profile if any.
	DeleteMachineCloudInstance(context.Context, string) error

	// IsController returns whether the machine is a controller machine.
	// It returns a NotFound if the given machine doesn't exist.
	IsController(context.Context, machine.Name) (bool, error)

	// RequireMachineReboot sets the machine referenced by its UUID as requiring a reboot.
	RequireMachineReboot(ctx context.Context, uuid string) error

	// CancelMachineReboot cancels the reboot of the machine referenced by its UUID if it has previously been required.
	CancelMachineReboot(ctx context.Context, uuid string) error

	// IsMachineRebootRequired checks if the machine referenced by its UUID requires a reboot.
	IsMachineRebootRequired(ctx context.Context, uuid string) (bool, error)
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
func (s *Service) CreateMachine(ctx context.Context, machineName machine.Name) (string, error) {
	// Make a new UUIDs for the net-node and the machine.
	// We want to do this in the service layer so that if retries are invoked at
	// the state layer we don't keep regenerating.
	nodeUUID, err := uuid.NewUUID()
	if err != nil {
		return "", errors.Annotatef(err, "creating machine %q", machineName)
	}
	machineUUID, err := uuid.NewUUID()
	if err != nil {
		return "", errors.Annotatef(err, "creating machine %q", machineName)
	}

	err = s.st.CreateMachine(ctx, machineName, nodeUUID.String(), machineUUID.String())

	return machineUUID.String(), errors.Annotatef(err, "creating machine %q", machineName)
}

// DeleteMachine deletes the specified machine.
func (s *Service) DeleteMachine(ctx context.Context, machineName machine.Name) error {
	err := s.st.DeleteMachine(ctx, machineName)
	return errors.Annotatef(err, "deleting machine %q", machineName)
}

// GetLife returns the GetMachineLife status of the specified machine.
// It returns a NotFound if the given machine doesn't exist.
func (s *Service) GetMachineLife(ctx context.Context, machineName machine.Name) (*life.Life, error) {
	life, err := s.st.GetMachineLife(ctx, machineName)
	return life, errors.Annotatef(err, "getting life status for machine %q", machineName)
}

// SetMachineLife sets the life status of the specified machine.
// It returns a NotFound if the provided machine doesn't exist.
func (s *Service) SetMachineLife(ctx context.Context, machineName machine.Name, life life.Life) error {
	err := s.st.SetMachineLife(ctx, machineName, life)
	return errors.Annotatef(err, "setting life status for machine %q", machineName)
}

// EnsureDeadMachine sets the provided machine's life status to Dead.
// No error is returned if the provided machine doesn't exist, just nothing gets
// updated.
func (s *Service) EnsureDeadMachine(ctx context.Context, machineName machine.Name) error {
	return s.SetMachineLife(ctx, machineName, life.Dead)
}

// AllMachineNames returns the names of all machines in the model.
func (s *Service) AllMachineNames(ctx context.Context) ([]machine.Name, error) {
	machines, err := s.st.AllMachineNames(ctx)
	if err != nil {
		return nil, errors.Annotate(err, "retrieving all machines")
	}
	return machines, nil
}

// InstanceId returns the cloud specific instance id for this machine.
// If the machine is not provisioned, it returns a NotProvisionedError.
func (s *Service) InstanceId(ctx context.Context, machineName machine.Name) (string, error) {
	instanceId, err := s.st.InstanceId(ctx, machineName)
	if err != nil {
		return "", errors.Annotatef(err, "retrieving cloud instance id for machine %q", machineName)
	}
	return instanceId, nil
}

// InstanceStatus returns the cloud specific instance status for this
// machine.
// If the machine is not provisioned, it returns a NotProvisionedError.
func (s *Service) InstanceStatus(ctx context.Context, machineName machine.Name) (string, error) {
	instanceStatus, err := s.st.InstanceStatus(ctx, machineName)
	if err != nil {
		return "", errors.Annotatef(err, "retrieving cloud instance status for machine %q", machineName)
	}
	return instanceStatus, nil
}

// IsController returns whether the machine is a controller machine.
// It returns a NotFound if the given machine doesn't exist.
func (s *Service) IsController(ctx context.Context, machineName machine.Name) (bool, error) {
	isController, err := s.st.IsController(ctx, machineName)
	if err != nil {
		return false, errors.Annotatef(err, "checking if machine %q is a controller", machineName)
	}
	return isController, nil
}

// RequireMachineReboot sets the machine referenced by its UUID as requiring a reboot.
func (s *Service) RequireMachineReboot(ctx context.Context, uuid string) error {
	return errors.Annotatef(s.st.RequireMachineReboot(ctx, uuid), "requiring a machine reboot for machine with uuid %q", uuid)
}

// CancelMachineReboot cancels the reboot of the machine referenced by its UUID if it has previously been required.
func (s *Service) CancelMachineReboot(ctx context.Context, uuid string) error {
	return errors.Annotatef(s.st.CancelMachineReboot(ctx, uuid), "cancelling a machine reboot for machine with uuid %q", uuid)
}

// IsMachineRebootRequired checks if the machine referenced by its UUID requires a reboot.
func (s *Service) IsMachineRebootRequired(ctx context.Context, uuid string) (bool, error) {
	rebootRequired, err := s.st.IsMachineRebootRequired(ctx, uuid)
	return rebootRequired, errors.Annotatef(err, "checking if machine with uuid %q is requiring a reboot", uuid)
}