// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"context"
	"strings"

	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/juju/juju/caas"
	"github.com/juju/juju/core/backups"
	"github.com/juju/juju/internal/docker/registry"
	"github.com/juju/juju/internal/errors"
)

// ControllerBackupProvisioning records observed images and storage rather than
// resolving mutable tags, which can already differ from the running source.
func (k *kubernetesClient) ControllerBackupProvisioning(ctx context.Context) (*backups.KubernetesProvisioning, error) {
	set, err := k.client().AppsV1().StatefulSets(k.namespace).Get(ctx, "controller", metav1.GetOptions{})
	if err != nil {
		return nil, errors.Capture(err)
	}
	pod, err := k.client().CoreV1().Pods(k.namespace).Get(ctx, "controller-0", metav1.GetOptions{})
	if err != nil {
		return nil, errors.Capture(err)
	}
	node, err := k.client().CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Capture(err)
	}
	result := &backups.KubernetesProvisioning{Architecture: node.Status.NodeInfo.Architecture}
	for _, container := range set.Spec.Template.Spec.Containers {
		if container.Name != apiServerContainerName && container.Name != "charm" {
			continue
		}
		var image string
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name != container.Name || status.State.Running == nil {
				continue
			}
			image, err = sourceImage(container.Image, status.ImageID)
			if err == nil {
				credentials, credErr := k.controllerImageCredentials(ctx, image)
				if credErr != nil {
					return nil, credErr
				}
				image, err = registry.VerifySourceReference(ctx, image, status.ImageID, credentials, result.Architecture)
			}
			if err != nil {
				return nil, errors.Errorf("source container %q: %w", container.Name, err)
			}
		}
		if container.Name == apiServerContainerName {
			result.AgentImage = image
		}
		if container.Name == "charm" {
			result.CharmImage = image
		}
	}
	if result.AgentImage == "" || result.CharmImage == "" || result.Architecture == "" {
		return nil, errors.New("source controller has no running agent, charm or platform identity")
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.Name != storageName || volume.PersistentVolumeClaim == nil {
			continue
		}
		claim, err := k.client().CoreV1().PersistentVolumeClaims(k.namespace).Get(ctx, volume.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
		if err != nil {
			return nil, errors.Capture(err)
		}
		if claim.Spec.StorageClassName == nil {
			return nil, errors.New("controller volume has no storage class")
		}
		result.StorageClass = *claim.Spec.StorageClassName
		capacity := claim.Status.Capacity[core.ResourceStorage]
		if capacity.IsZero() {
			capacity = claim.Spec.Resources.Requests[core.ResourceStorage]
		}
		if capacity.Sign() <= 0 {
			return nil, errors.New("controller volume has no capacity")
		}
		result.StorageSizeMiB = uint64((capacity.Value() + (1 << 20) - 1) >> 20)
	}
	if result.StorageSizeMiB == 0 {
		return nil, errors.New("source controller data volume is missing")
	}
	service, err := k.client().CoreV1().Services(k.namespace).Get(ctx, "controller-service", metav1.GetOptions{})
	if err != nil {
		return nil, errors.Capture(err)
	}
	switch service.Spec.Type {
	case core.ServiceTypeClusterIP:
		result.ServiceType = string(caas.ServiceCluster)
	case core.ServiceTypeLoadBalancer:
		result.ServiceType = string(caas.ServiceLoadBalancer)
	case core.ServiceTypeExternalName:
		result.ServiceType = string(caas.ServiceExternal)
	default:
		return nil, errors.Errorf("unsupported source controller service type %q", service.Spec.Type)
	}
	result.ExternalName, result.ExternalIPs = service.Spec.ExternalName, service.Spec.ExternalIPs
	result.LoadBalancerIP, result.ServiceAnnotations = service.Spec.LoadBalancerIP, service.Annotations
	return result, nil
}

func sourceImage(template, runtime string) (string, error) {
	for _, prefix := range []string{"docker-pullable://", "docker://", "containerd://", "cri-o://"} {
		runtime = strings.TrimPrefix(runtime, prefix)
	}
	if ref, err := reference.ParseNormalizedNamed(runtime); err == nil {
		if _, ok := ref.(reference.Canonical); ok {
			return ref.String(), nil
		}
	}
	ref, err := reference.ParseNormalizedNamed(template)
	if err != nil {
		return "", errors.Capture(err)
	}
	if canonical, ok := ref.(reference.Canonical); ok && digest.Digest(runtime).Validate() == nil {
		return canonical.String(), nil
	}
	// Some runtimes report an unqualified digest. Verify this candidate by
	// fetching a manifest by digest; an unpinned config ID must fail that check.
	observed := digest.Digest(runtime)
	if observed.Validate() == nil {
		candidate, err := reference.WithDigest(reference.TrimNamed(ref), observed)
		if err != nil {
			return "", err
		}
		return candidate.String(), nil
	}
	return "", errors.New("cannot establish the source manifest from its running image ID")
}
