// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package bootstrap

import (
	"github.com/juju/juju/cloud"
	coreerrors "github.com/juju/juju/core/errors"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/errors"
	coretools "github.com/juju/juju/internal/tools"
)

// IAASRecoveryParams holds the parameters for allocating a replacement
// machine controller from a recovery archive. It embeds the ordinary
// bootstrap parameters and adds the machine recovery inputs.
type IAASRecoveryParams struct {
	BootstrapParams

	// AvailableTools is the exact archived agent set for machine recovery.
	// It bypasses packaged-tool discovery and local tool building.
	AvailableTools coretools.List
}

// Validate validates the machine recovery parameters, including the shared
// bootstrap parameters and the machine recovery requirements.
func (p IAASRecoveryParams) Validate() error {
	if err := validateCommonRecoveryParams(p.BootstrapParams); err != nil {
		return err
	}
	if cloud.CloudIsCAAS(p.Cloud) {
		return errors.Errorf("machine recovery for a Kubernetes cloud %w", coreerrors.NotValid)
	}
	if p.BootstrapBase.Empty() {
		return errors.Errorf("recovery without a bootstrap base %w", coreerrors.NotValid)
	}
	if p.BootstrapConstraints.Arch == nil {
		return errors.Errorf("recovery without a bootstrap architecture %w", coreerrors.NotValid)
	}
	hasTool := false
	for _, tool := range p.AvailableTools {
		if tool == nil {
			continue
		}
		hasTool = true
		if tool.Version.Number != *p.AgentVersion {
			return errors.Errorf(
				"recovery tool version %q not matching agent version %q %w",
				tool.Version.Number, *p.AgentVersion, coreerrors.NotValid,
			)
		}
	}
	if !hasTool {
		return errors.Errorf("recovery without any agent tools %w", coreerrors.NotValid)
	}
	return nil
}

// K8sRecoveryParams holds the parameters for allocating a replacement
// Kubernetes controller namespace from a recovery archive.
type K8sRecoveryParams struct {
	BootstrapParams
}

// Validate validates the Kubernetes recovery parameters, including the shared
// bootstrap parameters and the Kubernetes recovery requirements.
func (p K8sRecoveryParams) Validate() error {
	if err := validateCommonRecoveryParams(p.BootstrapParams); err != nil {
		return err
	}
	if !cloud.CloudIsCAAS(p.Cloud) {
		return errors.Errorf("Kubernetes recovery for a machine cloud %w", coreerrors.NotValid)
	}
	if !p.BootstrapBase.Empty() {
		return errors.Errorf("recovery with a bootstrap base for a Kubernetes controller %w", coreerrors.NotValid)
	}
	return nil
}

func validateCommonRecoveryParams(p BootstrapParams) error {
	if err := p.Validate(); err != nil {
		return errors.Capture(err)
	}
	if p.AgentVersion == nil {
		return errors.Errorf("recovery without an agent version %w", coreerrors.NotValid)
	}
	if p.BuildAgent {
		return errors.Errorf("recovery with agent building enabled %w", coreerrors.NotValid)
	}
	return nil
}

// RecoverIAAS allocates a replacement machine controller instance from a
// recovery archive through the allocation stages shared with Bootstrap,
// preserving the archived agent version and base, and stops before installing
// an agent or initialising its database. A fresh controller bootstrap is done
// by Bootstrap.
//
// RecoverIAAS returns the replacement instance ID whenever the provider
// reports a successful allocation, even if a later check fails. In that case
// the error also names the instance for manual cleanup. If the provider itself
// fails, RecoverIAAS returns an empty ID and preserves the provider's error.
func RecoverIAAS(
	ctx environs.BootstrapContext,
	environ environs.BootstrapEnviron,
	args IAASRecoveryParams,
) (instance.Id, error) {
	if err := args.Validate(); err != nil {
		return "", errors.Errorf("validating recovery parameters: %w", err)
	}

	bootstrapParams := providerBootstrapParams(args.BootstrapParams)
	if _, err := prepareIAASBootstrap(ctx, environ, args.BootstrapParams, &bootstrapParams); err != nil {
		return "", errors.Capture(err)
	}
	bootstrapParams.AvailableTools = args.AvailableTools

	if err := prepareProviderBootstrap(ctx, environ, args.BootstrapParams, &bootstrapParams, *args.AgentVersion); err != nil {
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
	err = checkIAASRecoveryResult(args, result)
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

// RecoverK8s prepares the replacement Kubernetes controller bootstrap
// configuration and allocates its namespace, stopping before creating any
// controller workloads. A fresh controller bootstrap is done by Bootstrap.
func RecoverK8s(
	ctx environs.BootstrapContext,
	environ environs.BootstrapEnviron,
	args K8sRecoveryParams,
) error {
	if err := args.Validate(); err != nil {
		return errors.Errorf("validating recovery parameters: %w", err)
	}

	bootstrapParams := providerBootstrapParams(args.BootstrapParams)
	bootstrapConstraints, err := prepareCAASConstraints(ctx, environ, args.BootstrapParams)
	if err != nil {
		return errors.Capture(err)
	}
	bootstrapParams.BootstrapConstraints = bootstrapConstraints

	result, err := environ.Bootstrap(ctx, bootstrapParams)
	if err != nil {
		return errors.Capture(err)
	}
	if result == nil || result.CaasBootstrapFinalizer == nil {
		return errors.New("provider returned no Kubernetes controller finaliser")
	}

	podConfig, err := finishCAASBootstrap(ctx, environ, args.BootstrapParams, result, bootstrapConstraints)
	if err != nil {
		return errors.Capture(err)
	}
	ctx.Verbosef("Prepared replacement controller pod configuration for %q", podConfig.ControllerName)

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

// checkIAASRecoveryResult verifies that the provider allocated a replacement
// machine controller matching the archived base and architecture. The result
// is non-nil and identifies an allocated instance.
func checkIAASRecoveryResult(
	args IAASRecoveryParams,
	result *environs.BootstrapResult,
) error {
	if result.Base != args.BootstrapBase {
		return errors.New("replacement base differs from archived controller")
	}
	if result.Arch != *args.BootstrapConstraints.Arch {
		return errors.New("replacement architecture differs from archived agent")
	}
	return nil
}
