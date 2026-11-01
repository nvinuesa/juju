// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/core/backups"
	"github.com/juju/juju/core/base"
	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/core/version"
	"github.com/juju/juju/internal/cloudconfig/podcfg"
	"github.com/juju/juju/internal/docker/registry"
	coretesting "github.com/juju/juju/internal/testing"
)

type k8sRecoverySuite struct{ coretesting.BaseSuite }

func TestK8sRecoverySuite(t *testing.T) { tc.Run(t, &k8sRecoverySuite{}) }

type denyRecoveryRegistry struct{}

func (denyRecoveryRegistry) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected registry request")
}

func k8sRecoveryArchiveFiles(c *tc.C, format int, agent, charm string) map[string][]byte {
	files := recoveryArchiveFiles("4.1.0.2")
	var metadata map[string]any
	c.Assert(json.Unmarshal(files["juju-backup/metadata.json"], &metadata), tc.ErrorIsNil)
	metadata["FormatVersion"] = format
	metadata["Kubernetes"] = backups.KubernetesProvisioning{
		AgentImage: agent, CharmImage: charm, Architecture: "arm64",
		StorageClass: "source", StorageSizeMiB: 65536, ServiceType: "LoadBalancer",
	}
	var err error
	files["juju-backup/metadata.json"], err = json.Marshal(metadata)
	c.Assert(err, tc.ErrorIsNil)
	dump := string(files["juju-backup/dump/controller.yaml"])
	dump = strings.Replace(dump, "type: lxd", "type: kubernetes", 1)
	dump = strings.Replace(dump, "model_type_id: 0", "model_type_id: 1", 1)
	files["juju-backup/dump/controller.yaml"] = []byte(dump)
	modelDump := string(files["juju-backup/dump/models/ctrl-model-uuid.yaml"])
	modelDump += `  application_platform:
  - application_uuid: controller-app
    architecture_id: 1
    os_id: '0'
    channel: 24.04/stable
  architecture:
  - id: 1
    name: arm64
  os:
  - id: 0
    name: ubuntu
`
	files["juju-backup/dump/models/ctrl-model-uuid.yaml"] = []byte(modelDump)
	return files
}

func (s *k8sRecoverySuite) TestArchivedImagesSurvivePreflightAndPodConfiguration(c *tc.C) {
	s.PatchValue(&version.Current, semversion.MustParse("4.2.0.9"))
	s.PatchValue(&registry.DefaultTransport, denyRecoveryRegistry{})
	for _, images := range [][2]string{
		{"example.com/juju/agent:4.1.0.2", "example.com/juju/charm:local"},
		{"example.com/juju/agent@sha256:" + strings.Repeat("a", 64), "example.com/juju/charm@sha256:" + strings.Repeat("b", 64)},
	} {
		path, sum := writeRecoveryArchive(c, k8sRecoveryArchiveFiles(c, 3, images[0], images[1]))
		cmd := &recoveryCommand{archivePath: path, assertionPath: writeRecoveryAssertion(c, path, sum)}
		info, err := cmd.preflight(cmdtesting.Context(c))
		c.Assert(err, tc.ErrorIsNil)
		c.Check(info.Kubernetes.AgentImage, tc.Equals, images[0])
		c.Check(info.Kubernetes.CharmImage, tc.Equals, images[1])
		charmBase, err := base.ParseBaseFromString(info.ControllerCharmBase)
		c.Assert(err, tc.ErrorIsNil)
		pcfg := &podcfg.ControllerPodConfig{Bootstrap: &podcfg.BootstrapConfig{}}
		applyArchivedK8sProvisioning(pcfg, info, charmBase)
		c.Check(pcfg.AgentImage, tc.Equals, images[0])
		c.Check(pcfg.CharmImage, tc.Equals, images[1])
		c.Check(pcfg.JujuVersion.String(), tc.Equals, "4.1.0.2")
		c.Check(pcfg.CharmBase, tc.DeepEquals, charmBase)
		c.Assert(pcfg.RecoveryStorageClass, tc.NotNil)
		c.Check(*pcfg.RecoveryStorageClass, tc.Equals, "source")
	}
}

func (s *k8sRecoverySuite) TestPreflightRejectsInvalidArchivedImages(c *tc.C) {
	for _, test := range []struct{ agent, charm, message string }{
		{"", "example.com/charm:tag", "invalid archived controller agent image.*"},
		{"not a valid image", "example.com/charm:tag", "invalid archived controller agent image.*"},
		{"example.com/agent:tag", "", "invalid archived controller charm image.*"},
		{"example.com/agent:tag", "bad image", "invalid archived controller charm image.*"},
	} {
		path, sum := writeRecoveryArchive(c, k8sRecoveryArchiveFiles(c, 3, test.agent, test.charm))
		cmd := &recoveryCommand{archivePath: path, assertionPath: writeRecoveryAssertion(c, path, sum)}
		// Run reaches preflight before consulting the client store or provisioning.
		c.Check(cmd.Run(cmdtesting.Context(c)), tc.ErrorMatches, test.message)
	}
}

func (s *k8sRecoverySuite) TestPreflightRejectsOldKubernetesArchive(c *tc.C) {
	path, sum := writeRecoveryArchive(c, k8sRecoveryArchiveFiles(c, 2, "example.com/agent:tag", "example.com/charm:tag"))
	cmd := &recoveryCommand{archivePath: path, assertionPath: writeRecoveryAssertion(c, path, sum)}
	c.Check(cmd.Run(cmdtesting.Context(c)), tc.ErrorMatches, "Kubernetes recovery requires source provisioning metadata.*")
}

func (s *k8sRecoverySuite) TestPreflightRejectsMissingOrInconsistentProvisioning(c *tc.C) {
	for _, test := range []struct {
		provisioning any
		message      string
	}{
		{nil, "Kubernetes recovery requires source provisioning metadata.*"},
		{backups.KubernetesProvisioning{AgentImage: "repo/agent:tag", CharmImage: "repo/charm:tag", Architecture: "amd64", StorageSizeMiB: 1}, "source Kubernetes provisioning disagrees with the archived controller"},
		{backups.KubernetesProvisioning{AgentImage: "repo/agent:tag", CharmImage: "repo/charm:tag", Architecture: "arm64"}, "source Kubernetes provisioning disagrees with the archived controller"},
	} {
		files := k8sRecoveryArchiveFiles(c, 3, "repo/agent:tag", "repo/charm:tag")
		var metadata map[string]any
		c.Assert(json.Unmarshal(files["juju-backup/metadata.json"], &metadata), tc.ErrorIsNil)
		metadata["Kubernetes"] = test.provisioning
		var err error
		files["juju-backup/metadata.json"], err = json.Marshal(metadata)
		c.Assert(err, tc.ErrorIsNil)
		path, sum := writeRecoveryArchive(c, files)
		cmd := &recoveryCommand{archivePath: path, assertionPath: writeRecoveryAssertion(c, path, sum)}
		c.Check(cmd.Run(cmdtesting.Context(c)), tc.ErrorMatches, test.message)
	}
}
