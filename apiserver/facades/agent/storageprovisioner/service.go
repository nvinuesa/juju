// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storageprovisioner

import (
	"context"

	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/blockdevice"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/internal/storage"
)

// ControllerConfigService provides access to the controller configuration.
type ControllerConfigService interface {
	// ControllerConfig returns the config values for the controller.
	ControllerConfig(context.Context) (controller.Config, error)
}

// ModelConfigService is the interface that the provisioner facade uses to get
// the model config.
type ModelConfigService interface {
	// ModelConfig returns the current config for the model.
	ModelConfig(context.Context) (*config.Config, error)
}

// MachineService defines the methods that the facade assumes from the Machine
// service.
type MachineService interface {
	// EnsureDeadMachine sets the provided machine's life status to Dead.
	// No error is returned if the provided machine doesn't exist, just nothing
	// gets updated.
	EnsureDeadMachine(ctx context.Context, machineName machine.Name) error
	// GetMachineUUID returns the UUID of a machine identified by its name.
	// It returns a MachineNotFound if the machine does not exist.
	GetMachineUUID(ctx context.Context, name machine.Name) (string, error)
	// InstanceID returns the cloud specific instance id for this machine.
	InstanceID(ctx context.Context, mUUID string) (string, error)
	// InstanceName returns the cloud specific display name for this machine.
	InstanceName(ctx context.Context, mUUID string) (string, error)
	// HardwareCharacteristics returns the hardware characteristics of the
	// of the specified machine.
	HardwareCharacteristics(ctx context.Context, machineUUID string) (*instance.HardwareCharacteristics, error)
}

// BlockDeviceService instances can fetch and watch block devices on a machine.
type BlockDeviceService interface {
	// BlockDevices returns the block devices for a specified machine.
	BlockDevices(ctx context.Context, machineId string) ([]blockdevice.BlockDevice, error)
	// WatchBlockDevices returns a new NotifyWatcher watching for
	// changes to block devices associated with the specified machine.
	WatchBlockDevices(ctx context.Context, machineId string) (watcher.NotifyWatcher, error)
}

// StoragePoolGetter instances get a storage pool by name.
type StoragePoolGetter interface {
	// GetStoragePoolByName returns the storage pool with the specified name.
	GetStoragePoolByName(ctx context.Context, name string) (*storage.Config, error)
}
