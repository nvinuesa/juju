// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	stdtesting "testing"

	"github.com/juju/errors"
	"github.com/juju/tc"

	"github.com/juju/juju/api/jujuclient"
	caas "github.com/juju/juju/caas"
	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/core/instance"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/instances"
)

type bootstrapRestoreSuite struct{}

func TestBootstrapRestoreSuite(t *stdtesting.T) {
	tc.Run(t, &bootstrapRestoreSuite{})
}

func restoreMetadataJSON(agentVersion string) string {
	return `{` +
		`"ID":"20260930-120000.aabb-ccdd-eeff",` +
		`"FormatVersion":2,` +
		`"Checksum":"abc",` +
		`"ChecksumFormat":"SHA-256, hex encoded",` +
		`"Size":10,` +
		`"Stored":"0001-01-01T00:00:00Z",` +
		`"Started":"2026-09-30T12:00:00Z",` +
		`"Finished":"2026-09-30T12:00:34Z",` +
		`"Notes":"",` +
		`"ModelUUID":"ctrl-model-uuid",` +
		`"Machine":"0",` +
		`"Hostname":"myhost",` +
		`"Version":"` + agentVersion + `",` +
		`"ControllerUUID":"ctrl-uuid",` +
		`"HANodes":1,` +
		`"ControllerMachineID":"0",` +
		`"ControllerMachineInstanceID":"inst-1"` +
		`}` + "\n"
}

// restoreControllerDump renders one controller model plus the given
// workload models as (name, modelTypeID) pairs.
func restoreControllerDump(models ...[2]string) string {
	out := "payload:\n" +
		"  controller:\n" +
		"  - uuid: ctrl-uuid\n" +
		"    model_uuid: ctrl-model-uuid\n" +
		"    target_version: 4.1.0\n" +
		"    ca_cert: source-ca-cert\n" +
		"    ca_private_key: source-ca-key\n" +
		"  cloud:\n" +
		"  - uuid: cloud-1\n    name: lxd\n    cloud_type_id: 1\n    endpoint: ''\n    skip_tls_verify: false\n" +
		"  cloud_type:\n" +
		"  - id: 1\n    type: lxd\n" +
		"  model_type:\n" +
		"  - id: 0\n    type: iaas\n" +
		"  - id: 1\n    type: caas\n" +
		"  controller_config:\n" +
		"  - key: controller-name\n    value: source-ctrl\n" +
		"  model:\n" +
		"  - uuid: ctrl-model-uuid\n    name: controller\n    cloud_uuid: cloud-1\n" +
		"    model_type_id: 0\n    activated: true\n    life_id: 0\n    qualifier: ''\n"
	for i, m := range models {
		out += fmt.Sprintf(
			"  - uuid: model-uuid-%d\n    name: %s\n    cloud_uuid: cloud-1\n"+
				"    model_type_id: %s\n    activated: true\n    life_id: 0\n    qualifier: ''\n",
			i, m[0], m[1])
	}
	return out
}

func restoreArchiveFiles(agentVersion string, models ...[2]string) map[string][]byte {
	files := map[string][]byte{
		"juju-backup/metadata.json":                    []byte(restoreMetadataJSON(agentVersion)),
		"juju-backup/dump/controller.yaml":             []byte(restoreControllerDump(models...)),
		"juju-backup/dump/models/ctrl-model-uuid.yaml": []byte("payload:\n  net_node:\n  - uuid: nn\n"),
		"juju-backup/root.tar":                         []byte("blobs"),
	}
	for i := range models {
		files[fmt.Sprintf("juju-backup/dump/models/model-uuid-%d.yaml", i)] =
			[]byte("payload:\n  net_node:\n  - uuid: nn\n")
	}
	return files
}

func writeRestoreArchive(c *tc.C, files map[string][]byte) (string, string) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := files[name]
		c.Assert(tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg,
		}), tc.ErrorIsNil)
		_, err := tw.Write(data)
		c.Assert(err, tc.ErrorIsNil)
	}
	c.Assert(tw.Close(), tc.ErrorIsNil)
	c.Assert(gz.Close(), tc.ErrorIsNil)

	sum := sha256.Sum256(buf.Bytes())
	path := filepath.Join(c.MkDir(), "juju-backup.tar.gz")
	c.Assert(os.WriteFile(path, buf.Bytes(), 0o600), tc.ErrorIsNil)
	return path, hex.EncodeToString(sum[:])
}

func (s *bootstrapRestoreSuite) preflightCmd(c *tc.C, files map[string][]byte) *bootstrapCommand {
	path, sum := writeRestoreArchive(c, files)
	return &bootstrapCommand{RestorePath: path, RestoreSHA256: sum}
}

func (s *bootstrapRestoreSuite) TestPreflightNoRestore(c *tc.C) {
	cmd := &bootstrapCommand{}
	info, err := cmd.runRestorePreflight(cmdtesting.Context(c), jujucloud.Cloud{Type: "lxd"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(info, tc.IsNil)
}

func (s *bootstrapRestoreSuite) TestPreflightSuccess(c *tc.C) {
	cmd := s.preflightCmd(c, restoreArchiveFiles(jujuversion.Current.String(), [2]string{"workload", "0"}))

	info, err := cmd.runRestorePreflight(cmdtesting.Context(c), jujucloud.Cloud{Type: "lxd"})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(info, tc.NotNil)
	c.Check(info.ControllerUUID, tc.Equals, "ctrl-uuid")
	c.Check(info.ControllerName, tc.Equals, "source-ctrl")
	c.Check(info.CACert, tc.Equals, "source-ca-cert")
	c.Check(info.Models, tc.HasLen, 2)
}

func (s *bootstrapRestoreSuite) TestPreflightVersionMismatch(c *tc.C) {
	cmd := s.preflightCmd(c, restoreArchiveFiles("1.2.3", [2]string{"workload", "0"}))

	_, err := cmd.runRestorePreflight(cmdtesting.Context(c), jujucloud.Cloud{Type: "lxd"})
	c.Assert(err, tc.ErrorMatches,
		"archive was created by agent version 1.2.3 but this binary is "+jujuversion.Current.String()+".*")
}

func (s *bootstrapRestoreSuite) TestPreflightProviderFamilyMismatch(c *tc.C) {
	cmd := s.preflightCmd(c, restoreArchiveFiles(jujuversion.Current.String()))

	_, err := cmd.runRestorePreflight(cmdtesting.Context(c), jujucloud.Cloud{Type: "ec2"})
	c.Assert(err, tc.ErrorMatches,
		"archive comes from a .lxd. controller but the bootstrap cloud is .ec2.*")
}

func (s *bootstrapRestoreSuite) TestPreflightMixedModelTypes(c *tc.C) {
	cmd := s.preflightCmd(c, restoreArchiveFiles(jujuversion.Current.String(), [2]string{"k8sworkload", "1"}))

	_, err := cmd.runRestorePreflight(cmdtesting.Context(c), jujucloud.Cloud{Type: "lxd"})
	c.Assert(err, tc.ErrorMatches, "source controller has mixed model types.*")
}

func (s *bootstrapRestoreSuite) TestPreflightChecksumMismatch(c *tc.C) {
	path, _ := writeRestoreArchive(c, restoreArchiveFiles(jujuversion.Current.String()))
	cmd := &bootstrapCommand{RestorePath: path, RestoreSHA256: "deadbeef"}

	_, err := cmd.runRestorePreflight(cmdtesting.Context(c), jujucloud.Cloud{Type: "lxd"})
	c.Assert(err, tc.ErrorMatches, "invalid restore archive: archive checksum mismatch.*")
}

// cleanupStore records controller removals; the embedded interfaces stay
// nil because only RemoveController is called on the restore cleanup path.
type cleanupStore struct {
	jujuclient.ControllerUpdater
	jujuclient.ControllerGetter

	removed []string
}

func (s *cleanupStore) RemoveController(name string) error {
	s.removed = append(s.removed, name)
	return nil
}

// cleanupEnvironIAAS is a stub machine-cloud environ recording which
// instances the cleanup stopped.
type cleanupEnvironIAAS struct {
	environs.BootstrapEnviron

	stopped []instance.Id
}

func (e *cleanupEnvironIAAS) StartInstance(ctx context.Context, args environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
	return nil, errors.New("not implemented")
}

func (e *cleanupEnvironIAAS) StopInstances(ctx context.Context, ids ...instance.Id) error {
	e.stopped = append(e.stopped, ids...)
	return nil
}

func (e *cleanupEnvironIAAS) AllInstances(ctx context.Context) ([]instances.Instance, error) {
	return nil, nil
}

func (e *cleanupEnvironIAAS) AllRunningInstances(ctx context.Context) ([]instances.Instance, error) {
	return nil, nil
}

// cleanupEnvironCAAS is a stub Kubernetes environ recording the model
// destroy.
type cleanupEnvironCAAS struct {
	environs.BootstrapEnviron

	destroyCalled bool
}

func (e *cleanupEnvironCAAS) Destroy(ctx context.Context) error {
	e.destroyCalled = true
	return nil
}

func (e *cleanupEnvironCAAS) GetService(ctx context.Context, appName string, includeClusterIP bool) (*caas.Service, error) {
	return nil, nil
}

func (s *bootstrapRestoreSuite) TestCleanupFailedRestoreStopsReplacementInstance(c *tc.C) {
	env := &cleanupEnvironIAAS{}
	store := &cleanupStore{}
	resultErr := &environs.BootstrapInstanceError{
		InstanceID: "replacement-1",
		Err:        errors.New("restore load failed"),
	}

	err := cleanupFailedRestoreBootstrap("ctrl", env, resultErr,
		cmdtesting.Context(c), store)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(env.stopped, tc.DeepEquals, []instance.Id{"replacement-1"})
	c.Check(store.removed, tc.DeepEquals, []string{"ctrl"})
}

func (s *bootstrapRestoreSuite) TestCleanupFailedRestoreBeforeInstanceStart(c *tc.C) {
	// The failure carries no instance id: nothing was started, so
	// nothing is stopped cloud-side.
	env := &cleanupEnvironIAAS{}
	store := &cleanupStore{}

	err := cleanupFailedRestoreBootstrap("ctrl", env, errors.New("preflight failed"),
		cmdtesting.Context(c), store)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(env.stopped, tc.HasLen, 0)
	c.Check(store.removed, tc.DeepEquals, []string{"ctrl"})
}

func (s *bootstrapRestoreSuite) TestCleanupFailedRestoreCAASDestroysNamespace(c *tc.C) {
	env := &cleanupEnvironCAAS{}
	store := &cleanupStore{}

	err := cleanupFailedRestoreBootstrap("ctrl", env, errors.New("restore load failed"),
		cmdtesting.Context(c), store)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(env.destroyCalled, tc.IsTrue)
	c.Check(store.removed, tc.DeepEquals, []string{"ctrl"})
}

func (s *bootstrapRestoreSuite) TestValidateRestoreSHA256Format(c *tc.C) {
	valid := strings.Repeat("ab", 32)
	c.Check(validateRestoreSHA256(valid), tc.ErrorIsNil)
	c.Check(validateRestoreSHA256(strings.ToUpper(valid)), tc.ErrorIsNil)
	c.Check(validateRestoreSHA256("0123"), tc.ErrorMatches,
		"--restore-sha256 must be a SHA-256 checksum: 64 hexadecimal characters")
	c.Check(validateRestoreSHA256(strings.Repeat("zz", 32)), tc.ErrorMatches,
		"--restore-sha256 must be a SHA-256 checksum: 64 hexadecimal characters")
}
