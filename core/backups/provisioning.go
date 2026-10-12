// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package backups

// KubernetesProvisioning records the running controller's substrate. Images
// identify the source's actual manifests, independently of mutable version tags.
type KubernetesProvisioning struct {
	AgentImage         string
	CharmImage         string
	Architecture       string
	StorageClass       string
	StorageSizeMiB     uint64
	ServiceType        string
	ExternalName       string
	ExternalIPs        []string
	LoadBalancerIP     string
	ServiceAnnotations map[string]string
}
