// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package bootstrap

import (
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/core/arch"
	"github.com/juju/juju/core/base"
	coreerrors "github.com/juju/juju/core/errors"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/errors"
	coretools "github.com/juju/juju/internal/tools"
)

// MachineRecoveryParams contains only the inputs needed to allocate a
// replacement machine controller from a recovery archive. Identity restoration,
// agent installation and client registration belong to later recovery stages.
type MachineRecoveryParams struct {
	AllocationParams

	// AgentVersion is the exact archived controller agent version.
	AgentVersion semversion.Number

	// Arch is the exact archived controller architecture, enforced on both
	// the allocation request and the provider result.
	Arch string
}

// Validate checks the inputs for replacement machine allocation.
func (p MachineRecoveryParams) Validate() error {
	if cloud.CloudIsCAAS(p.Cloud) {
		return errors.Errorf("machine recovery for a Kubernetes cloud %w", coreerrors.NotValid)
	}
	if err := validateCommonRecoveryParams(p.AllocationParams, p.AgentVersion); err != nil {
		return err
	}
	if p.BootstrapBase.Empty() {
		return errors.Errorf("recovery without a controller base %w", coreerrors.NotValid)
	}
	if !arch.IsSupportedArch(p.Arch) {
		return errors.Errorf("recovery controller architecture %q %w", p.Arch, coreerrors.NotValid)
	}
	// The archived architecture is authoritative: an explicit constraint
	// naming a different one would silently provision the wrong platform.
	if p.BootstrapConstraints.Arch != nil && *p.BootstrapConstraints.Arch != "" &&
		*p.BootstrapConstraints.Arch != p.Arch {
		return errors.Errorf(
			"recovery controller architecture %q conflicts with constraint architecture %q %w",
			p.Arch, *p.BootstrapConstraints.Arch, coreerrors.NotValid)
	}
	return nil
}

// K8sRecoveryParams contains only the inputs needed to allocate a replacement
// Kubernetes controller namespace from a recovery archive. Identity
// restoration, workload deployment and client registration belong to later
// recovery stages.
type K8sRecoveryParams struct {
	AllocationParams

	// AgentVersion is the exact archived controller agent version.
	AgentVersion semversion.Number
}

// Validate checks the inputs for replacement Kubernetes namespace allocation.
func (p K8sRecoveryParams) Validate() error {
	if !cloud.CloudIsCAAS(p.Cloud) {
		return errors.Errorf("Kubernetes recovery for a machine cloud %w", coreerrors.NotValid)
	}
	if err := validateCommonRecoveryParams(p.AllocationParams, p.AgentVersion); err != nil {
		return err
	}
	if !p.BootstrapBase.Empty() {
		return errors.Errorf("recovery with a bootstrap base for a Kubernetes controller %w", coreerrors.NotValid)
	}
	return nil
}

func validateCommonRecoveryParams(p AllocationParams, agentVersion semversion.Number) error {
	if err := p.validateCommon(); err != nil {
		return err
	}
	if agentVersion == (semversion.Number{}) {
		return errors.Errorf("recovery without an agent version %w", coreerrors.NotValid)
	}
	return nil
}

// RecoverMachine allocates a replacement machine controller instance from a
// recovery archive through the allocation stages shared with Bootstrap,
// preserving the archived agent version and base, and stops before installing
// an agent or initialising its database. A fresh controller bootstrap is done
// by Bootstrap.
//
// RecoverMachine returns the replacement instance ID whenever the provider
// reports a successful allocation, even if a later check fails. In that case
// the error also names the instance for manual cleanup. If the provider itself
// fails, RecoverMachine returns an empty ID and preserves the provider's error.
func RecoverMachine(
	ctx environs.BootstrapContext,
	environ environs.BootstrapEnviron,
	args MachineRecoveryParams,
) (instance.Id, error) {
	if err := args.Validate(); err != nil {
		return "", errors.Errorf("validating recovery parameters: %w", err)
	}

	params := providerAllocationParams(args.AllocationParams)
	// The archived base is the only base a replacement may use; it is
	// trivially the supported set for this allocation.
	if len(params.SupportedBootstrapBases) == 0 {
		params.SupportedBootstrapBases = []base.Base{args.BootstrapBase}
	}
	// Pin the archived architecture, keeping every other archived
	// constraint so the replacement matches the source instance.
	params.BootstrapConstraints.Arch = &args.Arch
	// Merge the spaces implied by the archived controller space config,
	// exactly as fresh bootstrap does, before the constraints are
	// validated.
	params.BootstrapConstraints.Spaces = args.ControllerConfig.AsSpaceConstraints(
		params.BootstrapConstraints.Spaces)

	prep, err := prepareMachineAllocation(
		ctx, environ, params, args.MetadataDir, args.BootstrapImage)
	if err != nil {
		return "", errors.Capture(err)
	}

	// The provider allocation API uses tools to select an image. Adapt the
	// archived platform at that boundary; this is not an installable agent.
	// Recovery never calls the fresh-bootstrap finaliser returned below.
	bootstrapParams := prep.params
	bootstrapParams.AvailableTools = coretools.List{{Version: semversion.Binary{
		Number: args.AgentVersion, Release: args.BootstrapBase.OS, Arch: args.Arch,
	}}}
	bootstrapParams, err = prepareProviderAllocation(ctx, environ, args.CloudCredential, bootstrapParams, args.AgentVersion)
	if err != nil {
		return "", errors.Capture(err)
	}

	ctx.Verbosef("Starting new instance for replacement controller")
	result, err := environ.Bootstrap(ctx, bootstrapParams)
	if err != nil {
		// The provider enriches its own errors with any instance it
		// allocated before failing, so recovery returns them
		// unchanged.
		return "", err
	}
	if result == nil {
		return "", errors.New("provider returned no replacement instance details")
	}
	if result.InstanceID == "" {
		return "", errors.New("recovery provisioning returned no replacement instance")
	}
	allocated := result.InstanceID
	err = checkMachineRecoveryResult(args, result)
	if err == nil && IsContextDone(ctx) {
		err = Cancelled()
	}
	if err != nil {
		// Wrapping rather than replacing the error keeps its
		// classification, including the cancellation sentinel,
		// intact.
		return allocated, errors.Errorf(
			"replacement instance %q may require manual removal: %w",
			allocated, err,
		)
	}

	ctx.Infof("Replacement instance %q now allocated", allocated)
	return allocated, nil
}

// RecoverK8s prepares the replacement Kubernetes controller allocation inputs
// and creates its namespace, stopping before creating any controller
// workloads. A fresh controller bootstrap is done by Bootstrap.
func RecoverK8s(
	ctx environs.BootstrapContext,
	environ environs.BootstrapEnviron,
	args K8sRecoveryParams,
) error {
	if err := args.Validate(); err != nil {
		return errors.Errorf("validating recovery parameters: %w", err)
	}

	bootstrapParams := providerAllocationParams(args.AllocationParams)
	bootstrapConstraints, err := prepareK8sConstraints(ctx, environ, args.AllocationParams)
	if err != nil {
		return errors.Capture(err)
	}
	bootstrapParams.BootstrapConstraints = bootstrapConstraints

	if err := setBootstrapAgentVersion(ctx, environ, args.AgentVersion); err != nil {
		return errors.Capture(err)
	}

	result, err := environ.Bootstrap(ctx, bootstrapParams)
	if err != nil {
		return errors.Capture(err)
	}
	if result == nil || result.CaasBootstrapFinalizer == nil {
		return errors.New("provider returned no Kubernetes controller finaliser")
	}

	resources, ok := environ.(environs.ModelResources)
	if !ok {
		return errors.Errorf("Kubernetes recovery allocation for %T %w", environ, coreerrors.NotSupported)
	}
	if err := resources.CreateModelResources(ctx, environs.CreateParams{
		ControllerUUID: args.ControllerConfig.ControllerUUID(),
	}); err != nil {
		return errors.Capture(err)
	}
	if IsContextDone(ctx) {
		return Cancelled()
	}

	ctx.Infof("Replacement controller resources now allocated")
	return nil
}

// checkMachineRecoveryResult verifies that the provider allocated a
// replacement machine controller matching the archived base and architecture.
// The result is non-nil and identifies an allocated instance.
func checkMachineRecoveryResult(
	args MachineRecoveryParams,
	result *environs.BootstrapResult,
) error {
	if result.Base != args.BootstrapBase {
		return errors.New("replacement base differs from archived controller")
	}
	if result.Arch != args.Arch {
		return errors.New("replacement architecture differs from archived agent")
	}
	return nil
}
