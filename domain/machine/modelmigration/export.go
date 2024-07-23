// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelmigration

import (
	"context"

	"github.com/juju/description/v6"
	"github.com/juju/errors"
	"github.com/juju/names/v5"

	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/modelmigration"
	"github.com/juju/juju/domain/machine/service"
	"github.com/juju/juju/domain/machine/state"
)

// RegisterExport registers the export operations with the given coordinator.
func RegisterExport(coordinator Coordinator, logger logger.Logger) {
	coordinator.Add(&exportOperation{
		logger: logger,
	})
}

// ExportService defines the machine service used to export machines to
// another controller model to this controller.
type ExportService interface {
	// AllMachineUUIDs returns the UUIDs of all machines in the model.
	AllMachineUUIDs(ctx context.Context) ([]string, error)
	// InstanceID returns the cloud specific instance id for this machine.
	// If the machine is not provisioned, it returns a NotProvisionedError.
	InstanceID(ctx context.Context, machineUUID string) (string, error)
	// HardwareCharacteristics returns the hardware characteristics of the
	// of the specified machine.
	HardwareCharacteristics(ctx context.Context, machineUUID string) (*instance.HardwareCharacteristics, error)
}

// exportOperation describes a way to execute a migration for
// exporting external controllers.
type exportOperation struct {
	modelmigration.BaseOperation

	service ExportService
	logger  logger.Logger
}

// Name returns the name of this operation.
func (e *exportOperation) Name() string {
	return "export machines"
}

func (e *exportOperation) Setup(scope modelmigration.Scope) error {
	e.service = service.NewService(state.NewState(scope.ModelDB(), e.logger))
	return nil
}

func (e *exportOperation) Execute(ctx context.Context, model description.Model) error {
	// TODO(nvinuesa): We must retrieve all the machines full struct in one
	// transaction (i.e. GetAllMachines). Export of the full machine should
	// be implemented then.
	machineUUIDs, err := e.service.AllMachineUUIDs(ctx)
	if err != nil {
		return errors.Annotate(err, "retrieving all machines for export")
	}
	for _, machineUUID := range machineUUIDs {
		// TODO(nvinuesa): We must add machineUUID to description.
		machine := model.AddMachine(description.MachineArgs{
			Id: names.NewMachineTag(string(machineUUID)),
		})
		instanceID, err := e.service.InstanceID(ctx, machineUUID)
		if err != nil {
			return errors.Annotatef(err, "retrieving instance ID for machine %q", machineUUID)
		}
		instanceArgs := description.CloudInstanceArgs{
			InstanceId: instanceID,
		}
		// TODO(nvinuesa): machineUUID???
		machineUUID := ""
		hardwareCharacteristics, err := e.service.HardwareCharacteristics(ctx, machineUUID)
		if err != nil {
			return errors.Annotatef(err, "retrieving hardware characteristics for machine %q", machineUUID)
		}
		if hardwareCharacteristics.Arch != nil {
			instanceArgs.Architecture = *hardwareCharacteristics.Arch
		}
		if hardwareCharacteristics.Mem != nil {
			instanceArgs.Memory = *hardwareCharacteristics.Mem
		}
		if hardwareCharacteristics.RootDisk != nil {
			instanceArgs.RootDisk = *hardwareCharacteristics.RootDisk
		}
		if hardwareCharacteristics.RootDiskSource != nil {
			instanceArgs.RootDiskSource = *hardwareCharacteristics.RootDiskSource
		}
		if hardwareCharacteristics.CpuCores != nil {
			instanceArgs.CpuCores = *hardwareCharacteristics.CpuCores
		}
		if hardwareCharacteristics.CpuPower != nil {
			instanceArgs.CpuPower = *hardwareCharacteristics.CpuPower
		}
		if hardwareCharacteristics.Tags != nil {
			instanceArgs.Tags = *hardwareCharacteristics.Tags
		}
		if hardwareCharacteristics.AvailabilityZone != nil {
			instanceArgs.AvailabilityZone = *hardwareCharacteristics.AvailabilityZone
		}
		if hardwareCharacteristics.VirtType != nil {
			instanceArgs.VirtType = *hardwareCharacteristics.VirtType
		}
		machine.SetInstance(instanceArgs)
	}
	return nil
}
