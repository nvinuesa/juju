// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"context"

	core "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/provider/kubernetes/constants"
	"github.com/juju/juju/internal/provider/kubernetes/utils"
)

// CheckRestoreSubstrate implements environs.RestoreSubstrateChecker.
func (k *kubernetesClient) CheckRestoreSubstrate(ctx context.Context, params environs.RestoreSubstrateParams) error {
	return CheckRestoreSubstrate(ctx, k.client(), k.labelVersion, params)
}

// CheckRestoreSubstrate verifies the surviving-cluster substrate for a
// Kubernetes controller restore, read-only, before anything is
// provisioned:
//
//   - the controller namespace must be gone: the source controller is
//     fenced by scaling it to zero and deleting its namespace. A leftover
//     namespace with ready controller pods means the source is not
//     fenced; without ready pods it is debris the operator must remove.
//   - every workload model namespace must survive, annotated with the
//     archived controller and model UUIDs. Restore never recreates
//     namespaces.
func CheckRestoreSubstrate(
	ctx context.Context,
	client kubernetes.Interface,
	labelVersion constants.LabelVersion,
	params environs.RestoreSubstrateParams,
) error {
	controllerKey := utils.AnnotationControllerUUIDKey(labelVersion)
	modelKey := utils.AnnotationModelUUIDKey(labelVersion)

	controllerNamespace := DecideControllerNamespace(params.ControllerName)
	ns, err := client.CoreV1().Namespaces().Get(ctx, controllerNamespace, v1.GetOptions{})
	switch {
	case err == nil:
		ready, err := hasReadyControllerPods(ctx, client, ns.Name,
			constants.JujuControllerStackName, labelVersion)
		if err != nil {
			return errors.Capture(err)
		}
		if ready {
			return errors.Errorf(
				"source controller %q is not fenced: namespace %q still has ready controller pods; "+
					"scale its controller StatefulSet to zero and delete the namespace",
				params.ControllerName, controllerNamespace)
		}
		return errors.Errorf(
			"leftover controller namespace %q must be deleted before restore", controllerNamespace)
	case k8serrors.IsNotFound(err):
		// The expected state: bootstrap creates the namespace fresh.
	default:
		return errors.Errorf("checking controller namespace %q: %w", controllerNamespace, err)
	}

	for _, m := range params.Models {
		if m.UUID == params.ControllerModelUUID {
			// The controller model's namespace is the controller
			// namespace, recreated by bootstrap.
			continue
		}
		ns, err := client.CoreV1().Namespaces().Get(ctx, m.Name, v1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return errors.Errorf(
				"workload namespace %q for model %q is missing; restore never recreates namespaces",
				m.Name, m.Name)
		}
		if err != nil {
			return errors.Errorf("checking workload namespace %q: %w", m.Name, err)
		}
		if got := ns.Annotations[controllerKey]; got != params.ControllerUUID {
			return errors.Errorf(
				"workload namespace %q is annotated for controller %q, not the archived controller %q",
				m.Name, got, params.ControllerUUID)
		}
		if got := ns.Annotations[modelKey]; got != m.UUID {
			return errors.Errorf(
				"workload namespace %q is annotated for model %q, not the archived model %q",
				m.Name, got, m.UUID)
		}
	}
	return nil
}

// hasReadyControllerPods reports whether any pod matching the controller
// application's label selector in the namespace is ready. Only controller
// pods count as evidence the source is not fenced: a stray non-controller
// pod in the leftover namespace is debris, not a running controller.
func hasReadyControllerPods(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, appName string,
	labelVersion constants.LabelVersion,
) (bool, error) {
	selector := utils.SelectorLabelsForApp(appName, labelVersion)
	pods, err := client.CoreV1().Pods(namespace).List(ctx, v1.ListOptions{
		LabelSelector: labels.SelectorFromSet(selector).String(),
	})
	if err != nil {
		return false, errors.Errorf("listing pods in namespace %q: %w", namespace, err)
	}
	for _, pod := range pods.Items {
		for _, cond := range pod.Status.Conditions {
			if cond.Type == core.PodReady && cond.Status == core.ConditionTrue {
				return true, nil
			}
		}
	}
	return false, nil
}
