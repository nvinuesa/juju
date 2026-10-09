// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"context"
	"fmt"

	core "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	caask8s "github.com/juju/juju/caas/kubernetes"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/provider/kubernetes/constants"
	"github.com/juju/juju/internal/provider/kubernetes/utils"
)

// CheckRecoverySubstrate implements environs.RecoverySubstrateChecker.
func (k *kubernetesClient) CheckRecoverySubstrate(ctx context.Context, params environs.RecoverySubstrateParams) (*environs.RecoverySubstrateReport, error) {
	return CheckRecoverySubstrate(ctx, k.client(), k.labelVersion, params)
}

// PrepareForRecovery implements environs.RecoveryControllerPreparer. Workload
// namespaces belonging to the archived controller are expected to survive.
func (k *kubernetesClient) PrepareForRecovery(ctx environs.BootstrapContext, params environs.RecoverySubstrateParams) (*environs.RecoverySubstrateReport, error) {
	report, err := k.CheckRecoverySubstrate(ctx, params)
	if err != nil {
		return nil, errors.Capture(err)
	}
	k.namespace = DecideControllerNamespace(params.ControllerName)
	metadata, err := k.GetClusterMetadata(ctx, "")
	if err != nil {
		return nil, errors.Capture(err)
	}
	if metadata.WorkloadStorageClass == nil || metadata.WorkloadStorageClass.Name == "" {
		return nil, errors.New("controller storage class not identified")
	}
	report.HostCloudRegion = caask8s.K8sCloudOther
	if metadata.Cloud != "" {
		var region string
		if metadata.Regions != nil && metadata.Regions.Size() > 0 {
			region = metadata.Regions.SortedValues()[0]
		}
		report.HostCloudRegion = cloud.BuildHostCloudRegion(metadata.Cloud, region)
	}
	return report, nil
}

// CheckRecoverySubstrate verifies the surviving-cluster substrate for a
// Kubernetes controller recovery, read-only, before anything is
// provisioned:
//
//   - the controller namespace must be gone: the source controller is
//     fenced by scaling it to zero and deleting its namespace. A leftover
//     namespace with ready controller pods means the source is not
//     fenced; without ready pods it is debris the operator must remove.
//   - every workload model namespace must survive, annotated with the
//     archived controller and model UUIDs. Recovery never recreates
//     namespaces.
//   - every archived workload application's StatefulSet and every
//     archived volume claim must still exist. Missing workload substrate
//     is reported, never fatal: the first reconcile recreates missing
//     workload objects and a recreated claim comes back empty, so the
//     operator must know before the recovered controller starts.
func CheckRecoverySubstrate(
	ctx context.Context,
	client kubernetes.Interface,
	labelVersion constants.LabelVersion,
	params environs.RecoverySubstrateParams,
) (*environs.RecoverySubstrateReport, error) {
	controllerKey := utils.AnnotationControllerUUIDKey(labelVersion)
	modelKey := utils.AnnotationModelUUIDKey(labelVersion)

	controllerNamespace := DecideControllerNamespace(params.ControllerName)
	ns, err := client.CoreV1().Namespaces().Get(ctx, controllerNamespace, v1.GetOptions{})
	switch {
	case err == nil:
		ready, err := hasReadyControllerPods(ctx, client, ns.Name,
			constants.JujuControllerStackName, labelVersion)
		if err != nil {
			return nil, errors.Capture(err)
		}
		if ready {
			return nil, errors.Errorf(
				"source controller %q is not fenced: namespace %q still has ready controller pods; "+
					"scale its controller StatefulSet to zero and delete the namespace",
				params.ControllerName, controllerNamespace)
		}
		return nil, errors.Errorf(
			"leftover controller namespace %q must be deleted before recovery", controllerNamespace)
	case k8serrors.IsNotFound(err):
		// The expected state: bootstrap creates the namespace fresh.
	default:
		return nil, errors.Errorf("checking controller namespace %q: %w", controllerNamespace, err)
	}

	report := &environs.RecoverySubstrateReport{}
	for _, m := range params.Models {
		if m.UUID == params.ControllerModelUUID {
			// The controller model's namespace is the controller
			// namespace, recreated by bootstrap.
			continue
		}
		ns, err := client.CoreV1().Namespaces().Get(ctx, m.Name, v1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return nil, errors.Errorf(
				"workload namespace %q for model %q is missing; recovery never recreates namespaces",
				m.Name, m.Name)
		}
		if err != nil {
			return nil, errors.Errorf("checking workload namespace %q: %w", m.Name, err)
		}
		if got := ns.Annotations[controllerKey]; got != params.ControllerUUID {
			return nil, errors.Errorf(
				"workload namespace %q is annotated for controller %q, not the archived controller %q",
				m.Name, got, params.ControllerUUID)
		}
		if got := ns.Annotations[modelKey]; got != m.UUID {
			return nil, errors.Errorf(
				"workload namespace %q is annotated for model %q, not the archived model %q",
				m.Name, got, m.UUID)
		}

		for _, app := range m.Applications {
			// The application's StatefulSet is named after the
			// application in the model namespace. A missing one
			// is reported: the provisioner's first app.Ensure
			// recreates it with volume-claim templates.
			statefulSet, err := client.AppsV1().StatefulSets(m.Name).Get(
				ctx, app.Name, v1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				report.MissingWorkloads = append(
					report.MissingWorkloads, fmt.Sprintf("%s/%s", m.Name, app.Name))
			} else if err != nil {
				return nil, errors.Errorf(
					"checking workload statefulset %q in %q: %w", app.Name, m.Name, err)
			}
			if err == nil && (len(app.UUID) < 6 || statefulSet.Annotations[utils.AnnotationKeyApplicationUUID(labelVersion)] != app.UUID[:6]) {
				return nil, errors.Errorf("workload statefulset %q in %q belongs to another application", app.Name, m.Name)
			}

			for _, claim := range app.PersistentVolumeClaims {
				_, err := client.CoreV1().PersistentVolumeClaims(m.Name).Get(
					ctx, claim, v1.GetOptions{})
				if k8serrors.IsNotFound(err) {
					report.MissingPersistentVolumeClaims = append(
						report.MissingPersistentVolumeClaims, fmt.Sprintf("%s/%s", m.Name, claim))
				} else if err != nil {
					return nil, errors.Errorf(
						"checking volume claim %q in %q: %w", claim, m.Name, err)
				}
			}
		}
	}
	return report, nil
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
