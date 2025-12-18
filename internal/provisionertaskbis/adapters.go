// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertaskbis

import (
	"context"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v6"

	apiprovisioner "github.com/juju/juju/api/agent/provisioner"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/life"
	corelogger "github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/status"
	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/rpc/params"
)

// LegacyTaskConfig holds the configuration for creating a ProvisionerTask
// from legacy-style inputs. This matches the shape of the legacy TaskConfig
// to allow drop-in replacement.
type LegacyTaskConfig struct {
	ControllerUUID               string
	HostTag                      names.Tag
	Logger                       corelogger.Logger
	ControllerAPI                LegacyControllerAPI
	MachinesAPI                  LegacyMachinesAPI
	GetMachineInstanceInfoSetter LegacyGetMachineInstanceInfoSetter
	DistributionGroupFinder      LegacyDistributionGroupFinder
	ToolsFinder                  LegacyToolsFinder
	MachineWatcher               watcher.StringsWatcher
	RetryWatcher                 watcher.NotifyWatcher
	Broker                       environs.InstanceBroker
	ImageStream                  string
	RetryStartInstanceStrategy   RetryStrategy
	NumProvisionWorkers          int
	EventProcessedCb             func(string)
	AvailabilityZones            []string // Available zones for AZ Coordinator
}

// RetryStrategy defines retry behavior for provisioning.
type RetryStrategy struct {
	RetryDelay time.Duration
	RetryCount int
}

// LegacyMachinesAPI describes the API methods required for machine provisioning.
type LegacyMachinesAPI interface {
	Machines(context.Context, ...names.MachineTag) ([]apiprovisioner.MachineResult, error)
	MachinesWithTransientErrors(context.Context) ([]apiprovisioner.MachineStatusResult, error)
	WatchMachineErrorRetry(context.Context) (watcher.NotifyWatcher, error)
	WatchModelMachines(context.Context) (watcher.StringsWatcher, error)
	ProvisioningInfo(_ context.Context, machineTags []names.MachineTag) (params.ProvisioningInfoResults, error)
}

// LegacyControllerAPI describes API methods for querying a controller.
type LegacyControllerAPI interface {
	APIAddresses(context.Context) ([]string, error)
	CACert(context.Context) (string, error)
	ModelUUID(context.Context) (string, error)
}

// LegacyDistributionGroupFinder provides access to machine distribution groups.
type LegacyDistributionGroupFinder interface {
	DistributionGroupByMachineId(context.Context, ...names.MachineTag) ([]apiprovisioner.DistributionGroupResult, error)
}

// LegacyToolsFinder finds tools for machines.
type LegacyToolsFinder interface {
	// Not used directly in bis - placeholder for compatibility
}

// LegacyGetMachineInstanceInfoSetter provides the interface for setting instance info.
type LegacyGetMachineInstanceInfoSetter func(machineProvisioner apiprovisioner.MachineProvisioner) func(
	ctx context.Context,
	id instance.Id, displayName string, nonce string, characteristics *instance.HardwareCharacteristics,
	networkConfig []params.NetworkConfig, volumes []params.Volume,
	volumeAttachments map[string]params.VolumeAttachmentInfo, charmProfiles []string,
) error

// NewProvisionerTaskFromLegacy creates a ProvisionerTask from legacy-style config.
// This is the main entry point for using the bis implementation with existing Juju wiring.
func NewProvisionerTaskFromLegacy(cfg LegacyTaskConfig) (ProvisionerTask, error) {
	if cfg.Logger == nil {
		return nil, errors.NotValidf("nil Logger")
	}
	if cfg.MachinesAPI == nil {
		return nil, errors.NotValidf("nil MachinesAPI")
	}
	if cfg.Broker == nil {
		return nil, errors.NotValidf("nil Broker")
	}
	if cfg.MachineWatcher == nil {
		return nil, errors.NotValidf("nil MachineWatcher")
	}

	// Create logger adapter
	loggerAdapter := &loggerAdapter{logger: cfg.Logger}

	// Create machine getter adapter
	machineGetter := &machineGetterAdapter{
		api:    cfg.MachinesAPI,
		logger: loggerAdapter,
	}

	// Create broker adapter
	brokerAdapter := &brokerAdapter{
		broker: cfg.Broker,
		logger: loggerAdapter,
	}

	// Create instance info setter adapter
	infoSetterAdapter := &instanceInfoSetterAdapter{
		api:    cfg.MachinesAPI,
		getter: cfg.GetMachineInstanceInfoSetter,
		logger: loggerAdapter,
	}

	// Create semaphore
	semaphore := NewProviderSemaphore(cfg.NumProvisionWorkers)

	// Create AZ coordinator
	azCoordinator := NewAZCoordinator(cfg.AvailabilityZones, loggerAdapter)

	// Create task config
	taskCfg := TaskConfig{
		Logger:              loggerAdapter,
		MachineWatcher:      cfg.MachineWatcher,
		RetryWatcher:        cfg.RetryWatcher,
		MachineGetter:       machineGetter,
		Broker:              brokerAdapter,
		InstanceInfoSetter:  infoSetterAdapter,
		AZCoordinator:       azCoordinator,
		ProviderSemaphore:   semaphore,
		MaxRetries:          cfg.RetryStartInstanceStrategy.RetryCount,
		RetryDelay:          cfg.RetryStartInstanceStrategy.RetryDelay,
		NumProvisionWorkers: cfg.NumProvisionWorkers,
		EventProcessedCb:    cfg.EventProcessedCb,
	}

	return NewProvisionerTask(taskCfg)
}

// loggerAdapter adapts corelogger.Logger to our Logger interface.
type loggerAdapter struct {
	logger corelogger.Logger
}

func (l *loggerAdapter) Tracef(ctx context.Context, msg string, args ...any) {
	l.logger.Tracef(ctx, msg, args...)
}

func (l *loggerAdapter) Debugf(ctx context.Context, msg string, args ...any) {
	l.logger.Debugf(ctx, msg, args...)
}

func (l *loggerAdapter) Infof(ctx context.Context, msg string, args ...any) {
	l.logger.Infof(ctx, msg, args...)
}

func (l *loggerAdapter) Warningf(ctx context.Context, msg string, args ...any) {
	l.logger.Warningf(ctx, msg, args...)
}

func (l *loggerAdapter) Errorf(ctx context.Context, msg string, args ...any) {
	l.logger.Errorf(ctx, msg, args...)
}

// machineGetterAdapter adapts LegacyMachinesAPI to MachineGetter.
type machineGetterAdapter struct {
	api    LegacyMachinesAPI
	logger Logger
}

func (a *machineGetterAdapter) Machines(ctx context.Context, tags ...names.MachineTag) ([]MachineResult, error) {
	results, err := a.api.Machines(ctx, tags...)
	if err != nil {
		return nil, errors.Trace(err)
	}

	out := make([]MachineResult, len(results))
	for i, r := range results {
		if r.Err != nil {
			out[i] = MachineResult{Err: r.Err}
		} else {
			out[i] = MachineResult{
				Machine: &machineProvisionerAdapter{machine: r.Machine},
			}
		}
	}
	return out, nil
}

// machineProvisionerAdapter adapts apiprovisioner.MachineProvisioner to ClassifiableMachineFull.
type machineProvisionerAdapter struct {
	machine apiprovisioner.MachineProvisioner
}

func (a *machineProvisionerAdapter) Life() life.Value {
	return a.machine.Life()
}

func (a *machineProvisionerAdapter) Id() string {
	return a.machine.Id()
}

func (a *machineProvisionerAdapter) InstanceId(ctx context.Context) (string, error) {
	id, err := a.machine.InstanceId(ctx)
	if err != nil {
		if params.IsCodeNotProvisioned(err) {
			return "", errNotProvisioned
		}
		return "", err
	}
	return string(id), nil
}

func (a *machineProvisionerAdapter) EnsureDead(ctx context.Context) error {
	return a.machine.EnsureDead(ctx)
}

func (a *machineProvisionerAdapter) Status(ctx context.Context) (status.Status, string, error) {
	return a.machine.Status(ctx)
}

func (a *machineProvisionerAdapter) InstanceStatus(ctx context.Context) (status.Status, string, error) {
	return a.machine.InstanceStatus(ctx)
}

func (a *machineProvisionerAdapter) KeepInstance(ctx context.Context) (bool, error) {
	return a.machine.KeepInstance(ctx)
}

func (a *machineProvisionerAdapter) SetStatus(ctx context.Context, st status.Status, msg string, data map[string]interface{}) error {
	return a.machine.SetStatus(ctx, st, msg, data)
}

func (a *machineProvisionerAdapter) SetInstanceStatus(ctx context.Context, st status.Status, msg string, data map[string]interface{}) error {
	return a.machine.SetInstanceStatus(ctx, st, msg, data)
}

func (a *machineProvisionerAdapter) MarkForRemoval(ctx context.Context) error {
	return a.machine.MarkForRemoval(ctx)
}

func (a *machineProvisionerAdapter) Tag() names.MachineTag {
	return a.machine.MachineTag()
}

// brokerAdapter adapts environs.InstanceBroker to BrokerFacade.
type brokerAdapter struct {
	broker environs.InstanceBroker
	logger Logger
}

func (a *brokerAdapter) StartInstance(ctx context.Context, params StartInstanceParams) (StartInstanceResult, error) {
	// For now, this is a placeholder. The real implementation will need
	// to construct full environs.StartInstanceParams from the simplified params.
	// This will be done when the full wiring is implemented.
	a.logger.Debugf(ctx, "broker StartInstance called for machine %s in zone %s",
		params.MachineID, params.AvailabilityZone)

	// Placeholder: in real wiring, construct full params and call broker
	return StartInstanceResult{}, errors.NotImplementedf("full StartInstance wiring")
}

func (a *brokerAdapter) StopInstances(ctx context.Context, instanceIDs ...string) error {
	ids := make([]instance.Id, len(instanceIDs))
	for i, id := range instanceIDs {
		ids[i] = instance.Id(id)
	}
	return a.broker.StopInstances(ctx, ids...)
}

// instanceInfoSetterAdapter adapts the legacy instance info setter.
type instanceInfoSetterAdapter struct {
	api    LegacyMachinesAPI
	getter LegacyGetMachineInstanceInfoSetter
	logger Logger
}

func (a *instanceInfoSetterAdapter) SetInstanceInfo(ctx context.Context, machineID, instanceID, zoneName string) error {
	// For now, this is a placeholder. The real implementation will need
	// to fetch the machine and call the setter with full parameters.
	a.logger.Debugf(ctx, "SetInstanceInfo called for machine %s, instance %s, zone %s",
		machineID, instanceID, zoneName)

	// Placeholder: in real wiring, fetch machine and call setter
	return errors.NotImplementedf("full SetInstanceInfo wiring")
}

// AllRunningInstancesAdapter retrieves all running instances from a broker.
func AllRunningInstancesAdapter(ctx context.Context, broker environs.InstanceBroker) ([]InstanceInfo, error) {
	insts, err := broker.AllRunningInstances(ctx)
	if err != nil {
		return nil, errors.Trace(err)
	}

	result := make([]InstanceInfo, len(insts))
	for i, inst := range insts {
		result[i] = InstanceInfo{ID: string(inst.Id())}
	}
	return result, nil
}

// GetZoneFromInstance attempts to get the availability zone from an instance.
// Returns empty string if zones are not supported or zone cannot be determined.
func GetZoneFromInstance(inst instances.Instance) string {
	// Try to get zone info if instance supports it
	if zoned, ok := inst.(interface{ AvailabilityZone() string }); ok {
		return zoned.AvailabilityZone()
	}
	return ""
}
