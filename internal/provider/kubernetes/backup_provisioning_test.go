// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/juju/tc"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/juju/juju/internal/docker"
	"github.com/juju/juju/internal/docker/registry"
	"github.com/juju/juju/internal/provider/kubernetes/constants"
)

type backupProvisioningSuite struct{}

func TestBackupProvisioningSuite(t *testing.T) { tc.Run(t, &backupProvisioningSuite{}) }

func (s *backupProvisioningSuite) TestAnonymousHTTPImageEndpoint(c *tc.C) {
	client := fake.NewClientset()
	broker := &kubernetesClient{clientUnlocked: client, namespace: "controller-source"}
	details := docker.ImageRepoDetails{
		Repository: "localhost:5000/juju", ServerAddress: "http://localhost:5000",
	}
	c.Assert(broker.EnsureImageRepoSecret(c.Context(), details), tc.ErrorIsNil)
	image := "localhost:5000/juju/charm-base@sha256:" + strings.Repeat("a", 64)
	got, err := broker.controllerImageCredentials(c.Context(), image)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got.ServerAddress, tc.Equals, details.ServerAddress)
	c.Check(got.Repository, tc.Equals, "localhost:5000/juju/charm-base")
	c.Check(got.IsPrivate(), tc.IsFalse)

	// Check the persisted representation rather than only the return value.
	secret, err := client.CoreV1().Secrets(broker.Namespace()).Get(
		c.Context(), constants.CAASImageRepoSecretName, metav1.GetOptions{})
	c.Assert(err, tc.ErrorIsNil)
	var config struct {
		Auths map[string]docker.ImageRepoDetails `json:"auths"`
	}
	c.Assert(json.Unmarshal(secret.Data[core.DockerConfigJsonKey], &config), tc.ErrorIsNil)
	c.Check(config.Auths["localhost:5000"].ServerAddress, tc.Equals, details.ServerAddress)
}

func (s *backupProvisioningSuite) TestRunningImagesAndActualVolume(c *tc.C) {
	class := "original-class"
	config := `{"os":"linux","architecture":"arm64"}`
	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(config)))
	manifest := `{"schemaVersion":2,"config":{"digest":"` + configDigest + `"}}`
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(manifest)))
	originalTransport := registry.DefaultTransport
	c.Cleanup(func() { registry.DefaultTransport = originalTransport })
	registry.DefaultTransport = backupImageTransport(func(request *http.Request) (*http.Response, error) {
		body, media := manifest, "application/vnd.oci.image.manifest.v1+json"
		if strings.Contains(request.URL.Path, "/blobs/") {
			body, media = config, "application/octet-stream"
		}
		return &http.Response{Request: request, StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{media}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	client := fake.NewClientset(
		&apps.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "controller", Namespace: "controller-source"},
			Spec: apps.StatefulSetSpec{Template: core.PodTemplateSpec{Spec: core.PodSpec{Containers: []core.Container{
				{Name: apiServerContainerName, Image: "example.com/juju/jujud-operator:4.1.0"},
				{Name: "charm", Image: "example.com/juju/charm-base@" + digest},
			}}}}},
		&core.Pod{ObjectMeta: metav1.ObjectMeta{Name: "controller-0", Namespace: "controller-source"},
			Spec: core.PodSpec{NodeName: "worker", Volumes: []core.Volume{{Name: storageName,
				VolumeSource: core.VolumeSource{PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{ClaimName: "controller-storage"}},
			}}}, Status: core.PodStatus{ContainerStatuses: []core.ContainerStatus{
				{Name: apiServerContainerName, ImageID: "docker-pullable://example.com/juju/jujud-operator@" + digest,
					State: core.ContainerState{Running: &core.ContainerStateRunning{}}},
				{Name: "charm", ImageID: "containerd://" + digest,
					State: core.ContainerState{Running: &core.ContainerStateRunning{}}},
			}}},
		&core.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker"}, Status: core.NodeStatus{NodeInfo: core.NodeSystemInfo{Architecture: "arm64"}}},
		&core.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "controller-storage", Namespace: "controller-source"},
			Spec:   core.PersistentVolumeClaimSpec{StorageClassName: &class},
			Status: core.PersistentVolumeClaimStatus{Capacity: core.ResourceList{core.ResourceStorage: resource.MustParse("64Gi")}}},
		&core.Service{ObjectMeta: metav1.ObjectMeta{Name: "controller-service", Namespace: "controller-source", Annotations: map[string]string{"source": "retained"}},
			Spec: core.ServiceSpec{Type: core.ServiceTypeLoadBalancer, LoadBalancerIP: "192.0.2.1", ExternalIPs: []string{"192.0.2.2"}}},
	)
	broker := &kubernetesClient{clientUnlocked: client, namespace: "controller-source"}
	result, err := broker.ControllerBackupProvisioning(c.Context())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(result.AgentImage, tc.Equals, "example.com/juju/jujud-operator@"+digest)
	c.Check(result.CharmImage, tc.Equals, "example.com/juju/charm-base@"+digest)
	c.Check(result.Architecture, tc.Equals, "arm64")
	c.Check(result.StorageSizeMiB, tc.Equals, uint64(64*1024))
	c.Check(result.StorageClass, tc.Equals, class)
	c.Check(result.ServiceType, tc.Equals, "loadbalancer")
	c.Check(result.LoadBalancerIP, tc.Equals, "192.0.2.1")
	c.Check(result.ServiceAnnotations["source"], tc.Equals, "retained")
}

func (s *backupProvisioningSuite) TestAmbiguousSourceImageIsRejected(c *tc.C) {
	_, err := sourceImage("example.com/juju/jujud-operator:4.1.0", "containerd://unknown")
	c.Check(err, tc.ErrorMatches, "cannot establish the source manifest.*")
}

func (s *backupProvisioningSuite) TestRecoveryRequiresSourceStorageClass(c *tc.C) {
	broker := &kubernetesClient{clientUnlocked: fake.NewClientset()}
	class := "missing"
	_, err := broker.validateRecoveryControllerStorage(c.Context(), &class)
	c.Check(err, tc.ErrorMatches, "storageclasses.storage.k8s.io.*not found")
	_, err = broker.validateRecoveryControllerStorage(c.Context(), nil)
	c.Check(err, tc.ErrorMatches, "archive does not identify controller storage class")
	class = ""
	got, err := broker.validateRecoveryControllerStorage(c.Context(), &class)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.Equals, "")
}

type backupImageTransport func(*http.Request) (*http.Response, error)

func (f backupImageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func (s *backupProvisioningSuite) TestUnqualifiedRuntimeDigest(c *tc.C) {
	digest := "sha256:" + strings.Repeat("a", 64)
	ref, err := sourceImage("example.com/juju/jujud-operator:4.1.0", digest)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(ref, tc.Equals, "example.com/juju/jujud-operator@"+digest)
}
