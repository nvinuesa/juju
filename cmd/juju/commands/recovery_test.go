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
	"testing"

	"github.com/juju/errors"
	"github.com/juju/tc"

	"github.com/juju/juju/api/jujuclient"
	caas "github.com/juju/juju/caas"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/core/instance"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/instances"
)

type recoverySuite struct{}

func TestRecoverySuite(t *testing.T) { tc.Run(t, &recoverySuite{}) }

func (s *recoverySuite) TestInfo(c *tc.C) {
	info := newRecoveryCommand().Info()
	c.Check(info.Name, tc.Equals, "recovery")
	c.Check(info.Args, tc.Equals, "<backup-file>")
	c.Check(info.Examples, tc.Equals,
		"    juju recovery juju-backup.tar.gz --sha256 <checksum>")
}

func (s *recoverySuite) TestInit(c *tc.C) {
	for _, test := range []struct {
		args    []string
		pattern string
	}{
		{nil, "a backup archive is required"},
		{[]string{"backup.tar.gz"}, "--sha256 is required"},
		{[]string{"backup.tar.gz", "--sha256", "bad"}, "--sha256 must be.*"},
		{[]string{"backup.tar.gz", "lxd", "--sha256", strings.Repeat("a", 64)}, "unrecognized args.*"},
		{[]string{"backup.tar.gz", "--credential", "source"}, "(flag|option) provided but not defined.*"},
		{[]string{"backup.tar.gz", "--config", "ca-cert=override"}, "(flag|option) provided but not defined.*"},
	} {
		err := cmdtesting.InitCommand(newRecoveryCommand(), test.args)
		c.Check(err, tc.ErrorMatches, test.pattern)
	}
	err := cmdtesting.InitCommand(newRecoveryCommand(), []string{"backup.tar.gz", "--sha256", strings.Repeat("a", 64)})
	c.Assert(err, tc.ErrorIsNil)
}

func (s *recoverySuite) TestBootstrapRejectsRecoveryFlags(c *tc.C) {
	for _, flag := range []string{"--recovery", "--recovery-sha256"} {
		err := cmdtesting.InitCommand(newBootstrapCommand(), []string{"lxd", flag, "backup"})
		c.Check(err, tc.ErrorMatches, "(flag|option) provided but not defined.*")
	}
}

func (s *recoverySuite) TestRegisteredControllerRefused(c *tc.C) {
	path, sum := writeRecoveryArchive(c, recoveryArchiveFiles(jujuversion.Current.String()))
	store := jujuclient.NewMemStore()
	store.Controllers["source-ctrl"] = jujuclient.ControllerDetails{ControllerUUID: "other-controller"}
	command := &recoveryCommand{}
	command.SetClientStore(store)
	wrapped := modelcmd.Wrap(command, modelcmd.WrapSkipModelFlags, modelcmd.WrapSkipDefaultModel)
	_, err := cmdtesting.RunCommand(c, wrapped, path, "--sha256", sum)
	c.Check(err, tc.ErrorMatches, `controller "source-ctrl" is registered locally; run juju unregister source-ctrl before recovery`)
	c.Check(store.Controllers["source-ctrl"].ControllerUUID, tc.Equals, "other-controller")
}

func (s *recoverySuite) TestMissingSourceName(c *tc.C) {
	files := recoveryArchiveFiles(jujuversion.Current.String())
	files["juju-backup/dump/controller.yaml"] = []byte(strings.ReplaceAll(string(files["juju-backup/dump/controller.yaml"]), "value: source-ctrl", "value: ''"))
	path, sum := writeRecoveryArchive(c, files)
	_, err := (&recoveryCommand{archivePath: path, sha256: sum}).preflight(cmdtesting.Context(c))
	c.Check(err, tc.ErrorMatches, "recovery archive does not record the source controller name")
}

func (s *recoverySuite) TestArchivedTargetOverridesLocalCloud(c *tc.C) {
	info := &recovery.ArchiveInfo{Cloud: recovery.CloudInfo{
		Name: "custom", Type: "dummy", Endpoint: "https://source.example", SkipTLSVerify: true,
		AuthTypes: []string{"userpass"}, CACertificates: []string{"source-ca"},
		Regions: []recovery.RegionInfo{{Name: "source-region", Endpoint: "https://region.example"}},
	}}
	p := controllerProvisioner{Cloud: "unrelated-local-cloud", recoveryInfo: info}
	actual, _, err := p.cloud(cmdtesting.Context(c))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(actual.Name, tc.Equals, "custom")
	c.Check(actual.Endpoint, tc.Equals, "https://source.example")
	c.Check(actual.Regions[0].Name, tc.Equals, "source-region")
	c.Check(actual.CACertificates, tc.DeepEquals, []string{"source-ca"})
}

func (s *recoverySuite) TestCredentialSelection(c *tc.C) {
	provider, err := environs.Provider("dummy")
	c.Assert(err, tc.ErrorIsNil)
	provider = noDetectionProvider{provider}
	for _, mode := range []string{"local", "archived", "ambiguous", "revoked", "missing"} {
		store := jujuclient.NewMemStore()
		info := &recovery.ArchiveInfo{Region: "source-region", Credential: &recovery.CredentialInfo{
			Name: "archived", AuthType: string(cloud.UserPassAuthType), Attributes: map[string]string{"username": "source", "password": "secret"},
		}}
		if mode == "local" || mode == "ambiguous" {
			credentials := map[string]cloud.Credential{"local": cloud.NewCredential(cloud.UserPassAuthType, map[string]string{"username": "local", "password": "fresh"})}
			if mode == "ambiguous" {
				credentials["second"] = credentials["local"]
			}
			store.Credentials["dummy"] = cloud.CloudCredential{AuthCredentials: credentials}
		}
		if mode == "revoked" {
			info.Credential.Revoked = true
		}
		if mode == "missing" {
			info.Credential = nil
		}
		p := controllerProvisioner{recoveryInfo: info}
		p.SetClientStore(store)
		credentials, region, err := p.recoveryCredentials(cmdtesting.Context(c), provider, cloud.Cloud{
			Name: "dummy", Type: "dummy", AuthTypes: []cloud.AuthType{cloud.UserPassAuthType},
			Regions: []cloud.Region{{Name: "source-region"}},
		})
		switch mode {
		case "ambiguous":
			c.Check(err, tc.ErrorIs, modelcmd.ErrMultipleCredentials)
		case "revoked":
			c.Check(err, tc.ErrorMatches, "archived controller credential is revoked or invalid.*")
		case "missing":
			c.Check(err, tc.ErrorMatches, "no local or archived controller credential is available.*")
		default:
			c.Assert(err, tc.ErrorIsNil)
			c.Check(credentials.name, tc.Equals, mode)
			c.Check(region, tc.Equals, "source-region")
		}
	}
}

type failingRecoveryProvider struct{ environs.EnvironProvider }

func (p failingRecoveryProvider) FinalizeCredential(environs.FinalizeCredentialContext, environs.FinalizeCredentialParams) (*cloud.Credential, error) {
	return nil, errors.New("credential authentication failed")
}

func (s *recoverySuite) TestLocalCredentialErrorDoesNotFallBack(c *tc.C) {
	provider, err := environs.Provider("dummy")
	c.Assert(err, tc.ErrorIsNil)
	store := jujuclient.NewMemStore()
	// A selected local credential that fails authentication must not fall back.
	store.Credentials["dummy"] = cloud.CloudCredential{AuthCredentials: map[string]cloud.Credential{
		"local": cloud.NewCredential(cloud.UserPassAuthType, map[string]string{"username": "local", "password": "fresh"}),
	}}
	p := controllerProvisioner{recoveryInfo: &recovery.ArchiveInfo{Credential: &recovery.CredentialInfo{Name: "archived"}}}
	p.SetClientStore(store)
	_, _, err = p.recoveryCredentials(cmdtesting.Context(c), failingRecoveryProvider{provider}, cloud.Cloud{
		Name: "dummy", Type: "dummy", AuthTypes: []cloud.AuthType{cloud.UserPassAuthType},
	})
	c.Check(err, tc.ErrorMatches, ".*credential authentication failed.*")
}

type noDetectionProvider struct{ environs.EnvironProvider }

func (p noDetectionProvider) DetectCredentials(string) (*cloud.CloudCredential, error) {
	return nil, errors.NotFoundf("local credentials")
}

func recoveryMetadataJSON(agentVersion string) string {
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

// recoveryControllerDump renders one controller model plus the given
// workload models as (name, modelTypeID) pairs.
func recoveryControllerDump(models ...[2]string) string {
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

func recoveryArchiveFiles(agentVersion string, models ...[2]string) map[string][]byte {
	files := map[string][]byte{
		"juju-backup/metadata.json":                    []byte(recoveryMetadataJSON(agentVersion)),
		"juju-backup/dump/controller.yaml":             []byte(recoveryControllerDump(models...)),
		"juju-backup/dump/models/ctrl-model-uuid.yaml": []byte("payload:\n  net_node:\n  - uuid: nn\n"),
		"juju-backup/root.tar":                         []byte("blobs"),
	}
	for i := range models {
		files[fmt.Sprintf("juju-backup/dump/models/model-uuid-%d.yaml", i)] =
			[]byte("payload:\n  net_node:\n  - uuid: nn\n")
	}
	return files
}

func writeRecoveryArchive(c *tc.C, files map[string][]byte) (string, string) {
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

func (s *recoverySuite) preflightCmd(c *tc.C, files map[string][]byte) *recoveryCommand {
	path, sum := writeRecoveryArchive(c, files)
	return &recoveryCommand{archivePath: path, sha256: sum}
}

func (s *recoverySuite) TestPreflightSuccess(c *tc.C) {
	cmd := s.preflightCmd(c, recoveryArchiveFiles(jujuversion.Current.String(), [2]string{"workload", "0"}))

	info, err := cmd.preflight(cmdtesting.Context(c))
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(info, tc.NotNil)
	c.Check(info.ControllerUUID, tc.Equals, "ctrl-uuid")
	c.Check(info.ControllerName, tc.Equals, "source-ctrl")
	c.Check(info.CACert, tc.Equals, "source-ca-cert")
	c.Check(info.Models, tc.HasLen, 2)
}

func (s *recoverySuite) TestPreflightVersionMismatch(c *tc.C) {
	cmd := s.preflightCmd(c, recoveryArchiveFiles("1.2.3", [2]string{"workload", "0"}))

	_, err := cmd.preflight(cmdtesting.Context(c))
	c.Assert(err, tc.ErrorMatches,
		"archive was created by agent version 1.2.3 but this binary is "+jujuversion.Current.String()+".*")
}

func (s *recoverySuite) TestPreflightMixedModelTypes(c *tc.C) {
	cmd := s.preflightCmd(c, recoveryArchiveFiles(jujuversion.Current.String(), [2]string{"k8sworkload", "1"}))

	_, err := cmd.preflight(cmdtesting.Context(c))
	c.Assert(err, tc.ErrorMatches, "source controller has mixed model types.*")
}

func (s *recoverySuite) TestPreflightChecksumMismatch(c *tc.C) {
	path, _ := writeRecoveryArchive(c, recoveryArchiveFiles(jujuversion.Current.String()))
	cmd := &recoveryCommand{archivePath: path, sha256: "deadbeef"}

	_, err := cmd.preflight(cmdtesting.Context(c))
	c.Assert(err, tc.ErrorMatches, "invalid recovery archive: archive checksum mismatch.*")
}

// cleanupStore records controller removals; the embedded interfaces stay
// nil because only RemoveController is called on the recovery cleanup path.
type cleanupStore struct {
	jujuclient.ControllerUpdater
	jujuclient.ControllerGetter

	removed []string
}

func (s *cleanupStore) RemoveController(name string) error {
	s.removed = append(s.removed, name)
	return nil
}

// cleanupMachineEnviron is a stub machine-cloud environ recording which
// instances the cleanup stopped.
type cleanupMachineEnviron struct {
	environs.BootstrapEnviron

	stopped []instance.Id
}

func (e *cleanupMachineEnviron) StartInstance(ctx context.Context, args environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
	return nil, errors.New("not implemented")
}

func (e *cleanupMachineEnviron) StopInstances(ctx context.Context, ids ...instance.Id) error {
	e.stopped = append(e.stopped, ids...)
	return nil
}

func (e *cleanupMachineEnviron) AllInstances(ctx context.Context) ([]instances.Instance, error) {
	return nil, nil
}

func (e *cleanupMachineEnviron) AllRunningInstances(ctx context.Context) ([]instances.Instance, error) {
	return nil, nil
}

// cleanupK8sEnviron is a stub Kubernetes environ recording the model
// destroy.
type cleanupK8sEnviron struct {
	environs.BootstrapEnviron

	destroyCalled bool
}

func (e *cleanupK8sEnviron) Destroy(ctx context.Context) error {
	e.destroyCalled = true
	return nil
}

func (e *cleanupK8sEnviron) GetService(ctx context.Context, appName string, includeClusterIP bool) (*caas.Service, error) {
	return nil, nil
}

func (s *recoverySuite) TestCleanupFailedRecoveryStopsReplacementInstance(c *tc.C) {
	env := &cleanupMachineEnviron{}
	store := &cleanupStore{}
	resultErr := &environs.BootstrapInstanceError{
		InstanceID: "replacement-1",
		Err:        errors.New("recovery load failed"),
	}

	err := cleanupFailedRecovery("ctrl", env, resultErr, "",
		cmdtesting.Context(c), store)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(env.stopped, tc.DeepEquals, []instance.Id{"replacement-1"})
	c.Check(store.removed, tc.DeepEquals, []string{"ctrl"})
}

func (s *recoverySuite) TestCleanupFailedRecoveryUsesStashedInstanceID(c *tc.C) {
	// A late failure after the replacement started carries no
	// BootstrapInstanceError: the cleanup falls back to the instance id
	// the controller provisioner stashed and stops it.
	env := &cleanupMachineEnviron{}
	store := &cleanupStore{}

	err := cleanupFailedRecovery("ctrl", env, errors.New("agent init wait failed"),
		"replacement-9", cmdtesting.Context(c), store)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(env.stopped, tc.DeepEquals, []instance.Id{"replacement-9"})
	c.Check(store.removed, tc.DeepEquals, []string{"ctrl"})
}

func (s *recoverySuite) TestCleanupFailedRecoveryBeforeInstanceStart(c *tc.C) {
	// The failure carries no instance id and none was stashed: nothing
	// was started, so nothing is stopped cloud-side.
	env := &cleanupMachineEnviron{}
	store := &cleanupStore{}

	err := cleanupFailedRecovery("ctrl", env, errors.New("preflight failed"), "",
		cmdtesting.Context(c), store)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(env.stopped, tc.HasLen, 0)
	c.Check(store.removed, tc.DeepEquals, []string{"ctrl"})
}

func (s *recoverySuite) TestCleanupFailedRecoveryK8sDestroysNamespace(c *tc.C) {
	env := &cleanupK8sEnviron{}
	store := &cleanupStore{}

	err := cleanupFailedRecovery("ctrl", env, errors.New("recovery load failed"), "",
		cmdtesting.Context(c), store)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(env.destroyCalled, tc.IsTrue)
	c.Check(store.removed, tc.DeepEquals, []string{"ctrl"})
}

func (s *recoverySuite) TestValidateRecoverySHA256Format(c *tc.C) {
	valid := strings.Repeat("ab", 32)
	c.Check(validateRecoverySHA256(valid), tc.ErrorIsNil)
	c.Check(validateRecoverySHA256(strings.ToUpper(valid)), tc.ErrorIsNil)
	c.Check(validateRecoverySHA256("0123"), tc.ErrorMatches,
		"--sha256 must be a SHA-256 checksum: 64 hexadecimal characters")
	c.Check(validateRecoverySHA256(strings.Repeat("zz", 32)), tc.ErrorMatches,
		"--sha256 must be a SHA-256 checksum: 64 hexadecimal characters")
}
