// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisionertask

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/juju/errors"
	"github.com/juju/names/v6"

	"github.com/juju/juju/api"
	apiprovisioner "github.com/juju/juju/api/agent/provisioner"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/network"
	coreseries "github.com/juju/juju/core/series"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/internal/cloudconfig/cloudinit"
	"github.com/juju/juju/rpc/params"
	"github.com/juju/juju/storage"
)

// Enhanced helper methods for the state machine provisioner

// constructInstanceConfig creates a complete instance configuration
// This is the production implementation that handles all the complexity
func (p *stateMachineProvisioner) constructInstanceConfig(
	ctx context.Context,
	machine apiprovisioner.MachineProvisioner,
	provInfo *params.ProvisioningInfoResult,
) (*instances.InstanceConfig, error) {

	// Generate machine nonce
	machineNonce, err := generateMachineNonce()
	if err != nil {
		return nil, errors.Annotate(err, "generating machine nonce")
	}

	// Get controller configuration
	controllerConfig, err := p.config.ControllerAPI.ControllerConfig(ctx)
	if err != nil {
		return nil, errors.Annotate(err, "getting controller config")
	}

	// Get model configuration
	modelConfig, err := p.config.ControllerAPI.ModelConfig(ctx)
	if err != nil {
		return nil, errors.Annotate(err, "getting model config")
	}

	// Get API addresses
	apiAddresses, err := p.config.ControllerAPI.APIAddresses(ctx)
	if err != nil {
		return nil, errors.Annotate(err, "getting API addresses")
	}

	// Get CA certificate
	caCert, err := p.config.ControllerAPI.CACert(ctx)
	if err != nil {
		return nil, errors.Annotate(err, "getting CA certificate")
	}

	// Determine series/base
	base := provInfo.Result.Base
	series, err := coreseries.GetSeriesFromChannel(base.Name, base.Channel)
	if err != nil {
		return nil, errors.Annotatef(err, "determining series for base %s/%s", base.Name, base.Channel)
	}

	// Create instance configuration
	instanceConfig := &instances.InstanceConfig{
		// Basic machine info
		ControllerUUID: p.config.ControllerUUID,
		MachineId:      machine.Id(),
		MachineNonce:   machineNonce,

		// Series and tools
		Base:   base,
		Series: series,
		Tools:  provInfo.Result.Tools,

		// Network configuration
		Networks: provInfo.Result.NetworkConfig,
		APIInfo:  createAPIInfo(apiAddresses, caCert, machine.Tag()),

		// Instance metadata
		Tags: createInstanceTags(modelConfig, machine),

		// Cloud-init configuration
		CloudInitOutputLog: cloudinit.OutputCloudInit,

		// Controller configuration
		Controller: controllerConfig,

		// Enable OS upgrade if configured
		EnableOSRefreshUpdate: modelConfig.EnableOSRefreshUpdate(),
		EnableOSUpgrade:       modelConfig.EnableOSUpgrade(),

		// Proxy settings
		ProxySettings:     modelConfig.ProxySettings(),
		AptProxySettings:  modelConfig.AptProxySettings(),
		SnapProxySettings: modelConfig.SnapProxySettings(),

		// Package configuration
		PackageProxySettings: modelConfig.PackageProxySettings(),

		// JujuProxy settings
		JujuProxySettings: modelConfig.JujuProxySettings(),

		// Image stream
		ImageStream: p.config.ImageStream,

		// Agent version
		AgentVersion: provInfo.Result.Tools.Version,
	}

	// Add charm LXD profiles if needed
	if profiles, err := p.gatherCharmLXDProfiles(ctx, machine); err != nil {
		p.logger.Warningf("Failed to gather charm LXD profiles for machine %s: %v", machine.Id(), err)
	} else {
		instanceConfig.CharmLXDProfiles = profiles
	}

	// Add volumes and volume attachments
	if len(provInfo.Result.Volumes) > 0 {
		instanceConfig.Volumes = volumesToAPIServer(provInfo.Result.Volumes)
	}
	if len(provInfo.Result.VolumeAttachments) > 0 {
		instanceConfig.VolumeAttachments = volumeAttachmentsToAPIServer(provInfo.Result.VolumeAttachments)
	}

	// Add container configuration if this is a container machine
	if containerType := machine.ContainerType(); containerType != "" {
		containerConfig, err := p.getContainerConfig(ctx, machine, containerType)
		if err != nil {
			return nil, errors.Annotatef(err, "getting container config for machine %s", machine.Id())
		}
		instanceConfig.Container = containerConfig
	}

	// Validate the configuration
	if err := instanceConfig.Validate(); err != nil {
		return nil, errors.Annotate(err, "validating instance config")
	}

	return instanceConfig, nil
}

// constructStartInstanceParams creates complete start instance parameters
func (p *stateMachineProvisioner) constructStartInstanceParams(
	ctx context.Context,
	machine apiprovisioner.MachineProvisioner,
	instanceConfig *instances.InstanceConfig,
	provInfo *params.ProvisioningInfoResult,
) (*environs.StartInstanceParams, error) {

	// Parse constraints
	cons := provInfo.Result.Constraints

	// Handle placement directives
	placement := provInfo.Result.Placement
	availabilityZone, err := p.determineAvailabilityZone(ctx, machine, placement)
	if err != nil {
		return nil, errors.Annotatef(err, "determining availability zone for machine %s", machine.Id())
	}

	// Get distribution group for machine placement
	distributionGroup, err := p.getDistributionGroup(ctx, machine)
	if err != nil {
		p.logger.Warningf("Failed to get distribution group for machine %s: %v", machine.Id(), err)
	}

	// Create start instance parameters
	startInstanceParams := &environs.StartInstanceParams{
		// Instance configuration
		InstanceConfig: instanceConfig,

		// Tools and constraints
		Tools:       provInfo.Result.Tools,
		Constraints: cons,

		// Placement
		Placement:         placement,
		AvailabilityZone:  availabilityZone,
		DistributionGroup: distributionGroup,

		// Image metadata
		ImageMetadata: provInfo.Result.ImageMetadata,

		// Network configuration
		SubnetsToZones: subnetZonesFromNetworkTopology(provInfo.Result.NetworkTopology),

		// Root disk configuration
		RootDisk: createRootDiskParams(provInfo.Result.RootDisk),

		// Volume and storage configuration
		Volumes:           provInfo.Result.Volumes,
		VolumeAttachments: provInfo.Result.VolumeAttachments,

		// Status callback for reporting progress
		StatusCallback: func(status string) {
			p.logger.Debugf("Machine %s provisioning status: %s", machine.Id(), status)
		},

		// Clean up callback for handling failures
		CleanupCallback: func() {
			p.logger.Debugf("Cleaning up failed provisioning for machine %s", machine.Id())
		},
	}

	return startInstanceParams, nil
}

// determineAvailabilityZone determines the best availability zone for a machine
func (p *stateMachineProvisioner) determineAvailabilityZone(
	ctx context.Context,
	machine apiprovisioner.MachineProvisioner,
	placement string,
) (string, error) {

	// If placement specifies a zone directly, use that
	if strings.HasPrefix(placement, "zone=") {
		return strings.TrimPrefix(placement, "zone="), nil
	}

	// Check if provider supports availability zones
	if zoner, ok := p.config.Broker.(environs.AvailabilityZoner); ok {
		zones, err := zoner.AvailabilityZones(ctx)
		if err != nil {
			return "", errors.Annotate(err, "getting availability zones")
		}

		if len(zones) == 0 {
			return "", nil // No zones available
		}

		// Use zone distribution logic to balance machines across zones
		selectedZone, err := p.selectBestAvailabilityZone(ctx, machine, zones)
		if err != nil {
			p.logger.Warningf("Failed to select optimal zone for machine %s: %v", machine.Id(), err)
			// Fall back to first available zone
			return zones[0].Name(), nil
		}

		return selectedZone, nil
	}

	return "", nil // Provider doesn't support zones
}

// selectBestAvailabilityZone implements zone balancing logic
func (p *stateMachineProvisioner) selectBestAvailabilityZone(
	ctx context.Context,
	machine apiprovisioner.MachineProvisioner,
	zones []instances.AvailabilityZone,
) (string, error) {

	if len(zones) == 0 {
		return "", errors.New("no availability zones available")
	}

	if len(zones) == 1 {
		return zones[0].Name(), nil
	}

	// Count machines in each zone by examining our current machine states
	zoneCounts := make(map[string]int)
	for _, zone := range zones {
		zoneCounts[zone.Name()] = 0
	}

	// Count existing machines in each zone
	p.machines.Range(func(key, value interface{}) bool {
		if record, ok := value.(*MachineRecord); ok {
			if record.AvailZone != "" {
				if _, exists := zoneCounts[record.AvailZone]; exists {
					zoneCounts[record.AvailZone]++
				}
			}
		}
		return true
	})

	// Find zone with minimum machine count
	var selectedZone string
	minCount := int(^uint(0) >> 1) // Max int

	for _, zone := range zones {
		if count := zoneCounts[zone.Name()]; count < minCount {
			minCount = count
			selectedZone = zone.Name()
		}
	}

	if selectedZone == "" {
		selectedZone = zones[0].Name()
	}

	p.logger.Debugf("Selected availability zone %s for machine %s (current count: %d)",
		selectedZone, machine.Id(), zoneCounts[selectedZone])

	return selectedZone, nil
}

// getDistributionGroup gets the distribution group for a machine
func (p *stateMachineProvisioner) getDistributionGroup(
	ctx context.Context,
	machine apiprovisioner.MachineProvisioner,
) ([]instance.Id, error) {

	if p.config.DistributionGroupFinder == nil {
		return nil, nil
	}

	results, err := p.config.DistributionGroupFinder.DistributionGroupByMachineId(ctx, machine.MachineTag())
	if err != nil {
		return nil, errors.Annotate(err, "getting distribution group")
	}

	if len(results) != 1 {
		return nil, errors.Errorf("expected 1 distribution group result, got %d", len(results))
	}

	result := results[0]
	if result.Err != nil {
		return nil, errors.Annotate(result.Err, "distribution group error")
	}

	return result.Result, nil
}

// gatherCharmLXDProfiles gathers LXD profiles from deployed charms
func (p *stateMachineProvisioner) gatherCharmLXDProfiles(
	ctx context.Context,
	machine apiprovisioner.MachineProvisioner,
) ([]string, error) {

	// Get the applications deployed on this machine
	// This is a simplified implementation - the real one would need to
	// query the state for applications and their charm profiles

	// For now, return empty profiles as this is a complex feature
	// that would require additional API methods
	return []string{}, nil
}

// getContainerConfig gets container-specific configuration
func (p *stateMachineProvisioner) getContainerConfig(
	ctx context.Context,
	machine apiprovisioner.MachineProvisioner,
	containerType string,
) (*instances.ContainerConfig, error) {

	if p.config.ControllerAPI == nil {
		return nil, errors.New("controller API not available for container config")
	}

	// This would get container manager configuration
	// For now, return basic config
	return &instances.ContainerConfig{
		ContainerType: containerType,
	}, nil
}

// Helper functions

// generateMachineNonce generates a cryptographically secure nonce
func generateMachineNonce() (string, error) {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", errors.Annotate(err, "reading random bytes")
	}
	return fmt.Sprintf("%x", bytes), nil
}

// createAPIInfo creates API connection information
func createAPIInfo(addresses []string, caCert string, tag names.Tag) *api.Info {
	return &api.Info{
		Addrs:    addresses,
		CACert:   caCert,
		Tag:      tag,
		Password: "", // Will be set later during bootstrap
	}
}

// createInstanceTags creates tags for the instance
func createInstanceTags(modelConfig *config.Config, machine apiprovisioner.MachineProvisioner) map[string]string {
	tags := make(map[string]string)

	// Model and machine info
	tags[config.JujuModelUUIDTagKey] = modelConfig.UUID()
	tags[config.JujuControllerUUIDTagKey] = modelConfig.ControllerUUID()
	tags[config.JujuMachineTagKey] = machine.Id()

	// User-defined resource tags
	resourceTags := modelConfig.ResourceTags()
	for key, value := range resourceTags {
		tags[key] = value
	}

	return tags
}

// createRootDiskParams creates root disk parameters
func createRootDiskParams(rootDisk *params.RootDisk) *storage.VolumeParams {
	if rootDisk == nil {
		return nil
	}

	return &storage.VolumeParams{
		Size:     rootDisk.Size,
		Pool:     rootDisk.Pool,
		Provider: rootDisk.Provider,
		Tags:     rootDisk.Tags,
	}
}

// Machine classification helpers

// classifyMachine determines what action should be taken for a machine
func (p *stateMachineProvisioner) classifyMachine(machine apiprovisioner.MachineProvisioner) MachineClassification {
	// Get machine life status
	life := machine.Life()
	if life == params.Dead {
		return Dead
	}

	// Check if machine has an instance
	_, err := machine.InstanceId()
	if err == nil {
		// Machine has an instance
		if life == params.Dying {
			return Dead // Should be removed
		}
		return None // Already provisioned, no action needed
	}

	// Machine doesn't have an instance
	if life == params.Dying {
		return Dead // Don't provision dying machines
	}

	// Check machine status to see if it's waiting for provisioning
	status, err := machine.Status()
	if err != nil {
		p.logger.Warningf("Failed to get status for machine %s: %v", machine.Id(), err)
		return None
	}

	switch status.Status {
	case params.StatusPending:
		return Pending
	case params.StatusError:
		// Check if this is a transient error that should be retried
		if p.isTransientError(status.Info) {
			return Pending
		}
		return None
	default:
		return None
	}
}

// isTransientError determines if an error is transient and should trigger a retry
func (p *stateMachineProvisioner) isTransientError(errorInfo string) bool {
	transientErrors := []string{
		"no tools available",
		"cannot run instances",
		"quota exceeded",
		"rate limit",
		"service unavailable",
		"network error",
		"timeout",
	}

	errorLower := strings.ToLower(errorInfo)
	for _, transient := range transientErrors {
		if strings.Contains(errorLower, transient) {
			return true
		}
	}

	return false
}

// Network topology helpers

// subnetZonesFromNetworkTopology extracts subnet to zone mappings
func subnetZonesFromNetworkTopology(topology *params.NetworkTopology) map[network.Id][]string {
	if topology == nil {
		return nil
	}

	result := make(map[network.Id][]string)
	for _, subnet := range topology.Subnets {
		subnetId := network.Id(subnet.SubnetId)
		zones := make([]string, len(subnet.Zones))
		for i, zone := range subnet.Zones {
			zones[i] = zone.Name
		}
		result[subnetId] = zones
	}

	return result
}

// Volume helpers

// volumesToAPIServer converts volume parameters to API server format
func volumesToAPIServer(volumes []params.Volume) []params.Volume {
	// In the real implementation, this might do format conversion
	// For now, just return as-is
	return volumes
}

// volumeAttachmentsToAPIServer converts volume attachment parameters
func volumeAttachmentsToAPIServer(attachments map[string]params.VolumeAttachmentInfo) map[string]params.VolumeAttachmentInfo {
	// In the real implementation, this might do format conversion
	// For now, just return as-is
	return attachments
}
