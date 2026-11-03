// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/juju/errors"
	"github.com/juju/tc"
	"gopkg.in/yaml.v3"

	"github.com/juju/juju/api/jujuclient"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/cmd/juju/backups"
	"github.com/juju/juju/cmd/modelcmd"
	corebackups "github.com/juju/juju/core/backups"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	coretesting "github.com/juju/juju/internal/testing"
)

type recoverySuite struct {
	coretesting.BaseSuite
}

func TestRecoverySuite(t *testing.T) { tc.Run(t, &recoverySuite{}) }

// TestInit verifies that argument validation rejects the failing
// shapes in the table, and that both given paths are recorded as
// provided for recovery. An empty archive argument is reported before
// leftover arguments are rejected: it is the more useful error.
func (s *recoverySuite) TestInit(c *tc.C) {
	for _, test := range []struct {
		args    []string
		pattern string
	}{
		{nil, "a backup archive is required"},
		{[]string{"backup.tar.gz"}, "a backup assertion file is required"},
		{[]string{"", "backup.assert"}, "a backup archive is required"},
		{[]string{"backup.tar.gz", ""}, "a backup assertion file is required"},
		{[]string{"", "backup.assert", "extra"}, "a backup archive is required"},
		{[]string{"backup.tar.gz", "backup.assert", "extra"}, "unrecognized args.*"},
		{[]string{"backup.tar.gz", "backup.assert", "--sha256", "bad"}, "(flag|option) provided but not defined.*"},
		{[]string{"backup.tar.gz", "backup.assert", "--credential", "source"}, "(flag|option) provided but not defined.*"},
		{[]string{"backup.tar.gz", "backup.assert", "--config", "ca-cert=override"}, "(flag|option) provided but not defined.*"},
	} {
		err := cmdtesting.InitCommand(newRecoveryCommand(), test.args)
		c.Check(err, tc.ErrorMatches, test.pattern)
	}
	// Both paths are recorded for recovery, as given.
	command := &recoveryCommand{}
	wrapped := modelcmd.Wrap(command, modelcmd.WrapSkipModelFlags, modelcmd.WrapSkipDefaultModel)
	err := cmdtesting.InitCommand(wrapped, []string{"backup.tar.gz", "elsewhere/backup.assert"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(command.archivePath, tc.Equals, "backup.tar.gz")
	c.Check(command.assertionPath, tc.Equals, "elsewhere/backup.assert")
}

func (s *recoverySuite) TestBootstrapRejectsRecoveryFlags(c *tc.C) {
	for _, flag := range []string{"--recovery", "--recovery-sha256"} {
		err := cmdtesting.InitCommand(newBootstrapCommand(), []string{"lxd", flag, "backup"})
		c.Check(err, tc.ErrorMatches, "(flag|option) provided but not defined.*")
	}
}

func (s *recoverySuite) TestRegisteredControllerRefused(c *tc.C) {
	path, sum := writeRecoveryArchive(c, recoveryArchiveFiles(jujuversion.Current.String()))
	assertion := writeRecoveryAssertion(c, path, sum)
	store := jujuclient.NewMemStore()
	store.Controllers["source-ctrl"] = jujuclient.ControllerDetails{ControllerUUID: "other-controller"}
	command := &recoveryCommand{}
	command.SetClientStore(store)
	wrapped := modelcmd.Wrap(command, modelcmd.WrapSkipModelFlags, modelcmd.WrapSkipDefaultModel)
	_, err := cmdtesting.RunCommand(c, wrapped, path, assertion)
	c.Check(err, tc.ErrorMatches, `controller "source-ctrl" is registered locally; run juju unregister source-ctrl before recovery`)
	c.Check(store.Controllers["source-ctrl"].ControllerUUID, tc.Equals, "other-controller")
}

func (s *recoverySuite) TestMissingSourceName(c *tc.C) {
	files := recoveryArchiveFiles(jujuversion.Current.String())
	files["juju-backup/dump/controller.yaml"] = []byte(strings.ReplaceAll(string(files["juju-backup/dump/controller.yaml"]), "value: source-ctrl", "value: ''"))
	path, sum := writeRecoveryArchive(c, files)
	assertion := writeRecoveryAssertion(c, path, sum)
	_, err := (&recoveryCommand{archivePath: path, assertionPath: assertion}).preflight(cmdtesting.Context(c))
	c.Check(err, tc.ErrorMatches, "recovery archive does not record the source controller name")
}

// TestRun drives the wrapped recover command through every Run path:
// a missing, empty, malformed or uncloseable assertion file, an
// assertion whose checksum does not match the archive, a missing
// archive, and a valid archive, which reaches this build's terminal
// error. The paths are given relative to the command's working
// directory, so every case also exercises the relative to absolute
// path resolution: the expected messages quote the resolved
// absolute paths.
func (s *recoverySuite) TestRun(c *tc.C) {
	archive := recoveryArchive(c)
	sum := sha256.Sum256(archive)
	checksum := corebackups.ChecksumSHA256(hex.EncodeToString(sum[:]))
	assertionYAML := "checksum: " + checksum +
		"\nchecksum-format: " + corebackups.ChecksumFormatSHA256 + "\n"
	// A well-formed assertion recording some other archive's digest.
	mismatchedAssertion := "checksum: " + corebackups.ChecksumSHA256(strings.Repeat("0", sha256.Size*2)) +
		"\nchecksum-format: " + corebackups.ChecksumFormatSHA256 + "\n"
	// The same archive pinned by an assertion whose digest is recorded
	// in uppercase hex, as create-backup may preserve the controller's
	// spelling.
	uppercaseAssertion := "checksum: sha256-" + strings.ToUpper(hex.EncodeToString(sum[:])) +
		"\nchecksum-format: " + corebackups.ChecksumFormatSHA256 + "\n"
	emptyAssertion := ""
	malformedAssertion := "checksum: [unclosed\n"
	closeErr := errors.New("input/output error")

	const (
		archiveName   = "backup.tar.gz"
		assertionName = "backup.assert"
	)

	for i, test := range []struct {
		summary        string
		assertion      *string
		assertionIsDir bool
		archive        bool
		archiveIsDir   bool
		archiveEmpty   bool
		newCommand     func() cmd.Command
		expect         func(archivePath, assertionPath string) string
	}{{
		summary: "assertion file is missing",
		expect: func(_, assertionPath string) string {
			return `reading backup assertion "` + regexp.QuoteMeta(assertionPath) +
				`": open ` + regexp.QuoteMeta(assertionPath) + `: no such file or directory`
		},
	}, {
		summary:   "assertion file is empty",
		assertion: &emptyAssertion,
		expect: func(_, assertionPath string) string {
			return `reading backup assertion "` + regexp.QuoteMeta(assertionPath) +
				`": backup assertion is empty`
		},
	}, {
		summary:   "assertion file is malformed",
		assertion: &malformedAssertion,
		expect: func(_, assertionPath string) string {
			return `reading backup assertion "` + regexp.QuoteMeta(assertionPath) +
				`": cannot parse backup assertion: .*`
		},
	}, {
		summary:        "assertion path is a directory",
		assertionIsDir: true,
		expect: func(_, assertionPath string) string {
			return `reading backup assertion "` + regexp.QuoteMeta(assertionPath) +
				`": cannot parse backup assertion: yaml: input error: read ` +
				regexp.QuoteMeta(assertionPath) + `: is a directory`
		},
	}, {
		summary:   "assertion file fails to close",
		assertion: &assertionYAML,
		newCommand: func() cmd.Command {
			command := &recoveryCommand{}
			command.CanClearCurrentModel = true
			base := command.Filesystem()
			command.SetFilesystem(&hookedOpenFS{
				Filesystem: base,
				open: func(name string) (modelcmd.ReadSeekCloser, error) {
					file, err := base.Open(name)
					if err != nil {
						return nil, err
					}
					return &failingCloseReader{ReadSeekCloser: file, err: closeErr}, nil
				},
			})
			return modelcmd.Wrap(command, modelcmd.WrapSkipModelFlags, modelcmd.WrapSkipDefaultModel)
		},
		expect: func(_, assertionPath string) string {
			return `reading backup assertion "` + regexp.QuoteMeta(assertionPath) +
				`": input/output error`
		},
	}, {
		summary:   "archive is missing",
		assertion: &assertionYAML,
		expect: func(archivePath, _ string) string {
			return `invalid recovery archive: opening archive: open ` +
				regexp.QuoteMeta(archivePath) + `: no such file or directory`
		},
	}, {
		summary:   "archive does not match the assertion checksum",
		assertion: &mismatchedAssertion,
		archive:   true,
		expect: func(_, _ string) string {
			return `invalid recovery archive: archive checksum mismatch: expected sha256 "` +
				corebackups.ChecksumSHA256(strings.Repeat("0", sha256.Size*2)) +
				`", archive is "` + checksum + `"`
		},
	}, {
		summary:      "archive path is a directory",
		assertion:    &assertionYAML,
		archiveIsDir: true,
		expect: func(archivePath, _ string) string {
			return `invalid recovery archive: reading archive: read ` +
				regexp.QuoteMeta(archivePath) + `: is a directory`
		},
	}, {
		summary:      "archive is empty",
		assertion:    &assertionYAML,
		archiveEmpty: true,
		expect: func(_, _ string) string {
			return `invalid recovery archive: reading archive: EOF`
		},
	}, {
		summary:   "valid archive stops at archived agent selection",
		assertion: &assertionYAML,
		archive:   true,
		expect: func(_, _ string) string {
			return `selecting archived controller agent: unexpected EOF`
		},
	}, {
		summary:   "assertion records the checksum in uppercase hex",
		assertion: &uppercaseAssertion,
		archive:   true,
		expect: func(_, _ string) string {
			return `selecting archived controller agent: unexpected EOF`
		},
	}} {
		c.Logf("test %d: %s", i, test.summary)
		dir := c.MkDir()
		assertionPath := filepath.Join(dir, assertionName)
		if test.assertionIsDir {
			err := os.Mkdir(assertionPath, 0o755)
			c.Assert(err, tc.ErrorIsNil)
		} else if test.assertion != nil {
			err := os.WriteFile(assertionPath, []byte(*test.assertion), 0o600)
			c.Assert(err, tc.ErrorIsNil)
		}
		archivePath := filepath.Join(dir, archiveName)
		var archiveInfo os.FileInfo
		if test.archiveIsDir {
			err := os.Mkdir(archivePath, 0o755)
			c.Assert(err, tc.ErrorIsNil)
		} else if test.archiveEmpty {
			err := os.WriteFile(archivePath, nil, 0o600)
			c.Assert(err, tc.ErrorIsNil)
		} else if test.archive {
			err := os.WriteFile(archivePath, archive, 0o600)
			c.Assert(err, tc.ErrorIsNil)
			archiveInfo, err = os.Stat(archivePath)
			c.Assert(err, tc.ErrorIsNil)
		}

		newCmd := newRecoveryCommand
		if test.newCommand != nil {
			newCmd = test.newCommand
		}
		_, err := cmdtesting.RunCommandInDir(c, newCmd(),
			[]string{archiveName, assertionName}, dir)
		c.Check(err, tc.ErrorMatches, test.expect(archivePath, assertionPath))

		// The command never writes its inputs: the archive, when
		// present, is byte-identical afterwards with its mode
		// unchanged, and the workspace neither gains nor loses files.
		want := make([]string, 0, 2)
		if test.archive || test.archiveIsDir || test.archiveEmpty {
			want = append(want, archiveName)
		}
		if test.archive {
			data, err := os.ReadFile(archivePath)
			c.Assert(err, tc.ErrorIsNil)
			c.Check(data, tc.DeepEquals, archive)
			info, err := os.Stat(archivePath)
			c.Assert(err, tc.ErrorIsNil)
			c.Check(info.Mode(), tc.Equals, archiveInfo.Mode())
		}
		if test.assertion != nil || test.assertionIsDir {
			want = append(want, assertionName)
		}
		entries, err := os.ReadDir(dir)
		c.Assert(err, tc.ErrorIsNil)
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		sort.Strings(want)
		c.Check(names, tc.DeepEquals, want)
	}
}

// hookedOpenFS delegates every filesystem operation, except Open, to
// the wrapped filesystem, so a test can return a handle that behaves
// in ways a regular file cannot, such as failing to close.
type hookedOpenFS struct {
	modelcmd.Filesystem
	open func(name string) (modelcmd.ReadSeekCloser, error)
}

// Open implements Filesystem.
func (f *hookedOpenFS) Open(name string) (modelcmd.ReadSeekCloser, error) {
	return f.open(name)
}

// failingCloseReader stands in for the assertion file, serving its
// contents normally but failing to close, as a failing filesystem
// can report.
type failingCloseReader struct {
	modelcmd.ReadSeekCloser
	err error
}

// Close implements io.Closer; the wrapped handle is closed regardless
// and the scripted failure is reported.
func (r *failingCloseReader) Close() error {
	_ = r.ReadSeekCloser.Close()
	return r.err
}

// recoveryArchive renders a minimal valid recovery archive: the
// provenance metadata, a controller dump recording one controller
// model on an lxd cloud, that model's database dump, and the
// data-directory files archive. It is the smallest archive the
// recover command's validation accepts, so reaching the terminal
// error proves the whole read and verify path ran.
func recoveryArchive(c *tc.C) []byte {
	const (
		controllerUUID      = "c0ffee00-1111-2222-3333-444455556666"
		controllerModelUUID = "c0ffee00-aaaa-2222-3333-444455556666"
	)
	metadata := `{` +
		`"ID":"20261104-120000.aabb-ccdd-eeff",` +
		`"FormatVersion":2,` +
		`"Stored":"0001-01-01T00:00:00Z",` +
		`"Started":"2026-11-04T12:00:00Z",` +
		`"Finished":"2026-11-04T12:00:34Z",` +
		`"Notes":"",` +
		`"ModelUUID":"` + controllerModelUUID + `",` +
		`"Machine":"0",` +
		`"Hostname":"myhost",` +
		`"Version":"4.1.0",` +
		`"ControllerUUID":"` + controllerUUID + `",` +
		`"HANodes":1,` +
		`"ControllerMachineID":"0",` +
		`"ControllerMachineInstanceID":"inst-10101010"` +
		`}` + "\n"
	controllerDump := `version: 4.1.0
payload:
  controller:
  - uuid: ` + controllerUUID + `
    model_uuid: ` + controllerModelUUID + `
    ca_cert: source-ca-cert
    ca_private_key: source-ca-key
  cloud:
  - uuid: cloud-lxd
    name: lxd
    cloud_type_id: 1
  cloud_type:
  - id: 1
    type: lxd
  model_type:
  - id: 0
    type: iaas
  controller_config:
  - key: controller-name
    value: source-ctrl
  model:
  - uuid: ` + controllerModelUUID + `
    name: controller
    cloud_uuid: cloud-lxd
    model_type_id: 0
`
	files := map[string][]byte{
		"juju-backup/metadata.json":        []byte(metadata),
		"juju-backup/dump/controller.yaml": []byte(controllerDump),
		"juju-backup/dump/models/" + controllerModelUUID + ".yaml": []byte(`payload:
  net_node:
  - uuid: nn
  application_controller:
  - application_uuid: controller-app
  unit:
  - uuid: controller-unit
    name: controller/0
    application_uuid: controller-app
    life_id: 0
    net_node_uuid: nn
  machine:
  - uuid: controller-machine
    name: "0"
    net_node_uuid: nn
    life_id: 0
`),
		"juju-backup/root.tar": []byte("blobs"),
	}

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
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o600,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}
		c.Assert(tw.WriteHeader(hdr), tc.ErrorIsNil)
		_, err := tw.Write(data)
		c.Assert(err, tc.ErrorIsNil)
	}
	c.Assert(tw.Close(), tc.ErrorIsNil)
	c.Assert(gz.Close(), tc.ErrorIsNil)
	return buf.Bytes()
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
		p := recoveryCommand{recoveryInfo: info}
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
	p := recoveryCommand{recoveryInfo: &recovery.ArchiveInfo{Credential: &recovery.CredentialInfo{Name: "archived"}}}
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
		"juju-backup/metadata.json":        []byte(recoveryMetadataJSON(agentVersion)),
		"juju-backup/dump/controller.yaml": []byte(recoveryControllerDump(models...)),
		"juju-backup/dump/models/ctrl-model-uuid.yaml": []byte(`payload:
  net_node:
  - uuid: nn
  application_controller:
  - application_uuid: controller-app
  unit:
  - uuid: controller-unit
    name: controller/0
    application_uuid: controller-app
    life_id: 0
    net_node_uuid: nn
  machine:
  - uuid: controller-machine
    name: "0"
    net_node_uuid: nn
    life_id: 0
`),
		"juju-backup/root.tar": []byte("blobs"),
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

// writeRecoveryAssertion writes a valid assertion for the archive,
// recording the given checksum, beside it.
func writeRecoveryAssertion(c *tc.C, archivePath, checksum string) string {
	data, err := yaml.Marshal(backups.Assertion{
		Checksum:       checksum,
		ChecksumFormat: "SHA-256",
	})
	c.Assert(err, tc.ErrorIsNil)
	path := archivePath + ".assert"
	c.Assert(os.WriteFile(path, data, 0o600), tc.ErrorIsNil)
	return path
}

// writeRecoveryAssertionData writes raw assertion content beside the
// archive, for tests that need malformed or incomplete assertions.
func writeRecoveryAssertionData(c *tc.C, archivePath, content string) string {
	path := archivePath + ".assert"
	c.Assert(os.WriteFile(path, []byte(content), 0o600), tc.ErrorIsNil)
	return path
}

func (s *recoverySuite) preflightCmd(c *tc.C, files map[string][]byte) *recoveryCommand {
	path, sum := writeRecoveryArchive(c, files)
	assertion := writeRecoveryAssertion(c, path, sum)
	return &recoveryCommand{archivePath: path, assertionPath: assertion}
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

func (s *recoverySuite) TestClientVersionDoesNotSelectAgent(c *tc.C) {
	for _, v := range []string{"4.0.0", "4.2.0.7"} {
		command := s.preflightCmd(c, recoveryArchiveFiles(v))
		info, err := command.preflight(cmdtesting.Context(c))
		c.Assert(err, tc.ErrorIsNil)
		c.Check(info.AgentVersion.String(), tc.Equals, v)
	}
}

func (s *recoverySuite) TestPreflightMixedModelTypes(c *tc.C) {
	cmd := s.preflightCmd(c, recoveryArchiveFiles(jujuversion.Current.String(), [2]string{"k8sworkload", "1"}))

	info, err := cmd.preflight(cmdtesting.Context(c))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(info.Models, tc.HasLen, 2)
	c.Check(info.Models[1].ModelType, tc.Equals, "caas")
}

// TestPreflightChecksumMismatch verifies that an assertion recording a
// wrong, but well-formed, checksum stops recovery before anything is
// provisioned.
func (s *recoverySuite) TestPreflightChecksumMismatch(c *tc.C) {
	path, _ := writeRecoveryArchive(c, recoveryArchiveFiles(jujuversion.Current.String()))
	assertion := writeRecoveryAssertion(c, path, strings.Repeat("0", 64))
	cmd := &recoveryCommand{archivePath: path, assertionPath: assertion}

	_, err := cmd.preflight(cmdtesting.Context(c))
	c.Assert(err, tc.ErrorMatches, "invalid recovery archive: archive checksum mismatch.*")
}

func (s *recoverySuite) TestPreflightMissingArchive(c *tc.C) {
	path := filepath.Join(c.MkDir(), "juju-backup.tar.gz")
	assertion := writeRecoveryAssertion(c, path, strings.Repeat("a", 64))
	cmd := &recoveryCommand{archivePath: path, assertionPath: assertion}

	_, err := cmd.preflight(cmdtesting.Context(c))
	c.Check(err, tc.ErrorMatches, "invalid recovery archive: .*")
}

// TestPreflightAssertionFailures verifies that an unreadable, empty or
// malformed assertion stops recovery before the archive is read.
func (s *recoverySuite) TestPreflightAssertionFailures(c *tc.C) {
	path, _ := writeRecoveryArchive(c, recoveryArchiveFiles(jujuversion.Current.String()))

	cmd := &recoveryCommand{archivePath: path, assertionPath: path + ".missing"}
	_, err := cmd.preflight(cmdtesting.Context(c))
	c.Check(err, tc.ErrorMatches, `reading backup assertion ".*": .*no such file or directory`)

	assertion := writeRecoveryAssertionData(c, path, "")
	cmd = &recoveryCommand{archivePath: path, assertionPath: assertion}
	_, err = cmd.preflight(cmdtesting.Context(c))
	c.Check(err, tc.ErrorMatches, `reading backup assertion ".*": backup assertion is empty`)

	assertion = writeRecoveryAssertionData(c, path, "checksum: [unclosed\n")
	cmd = &recoveryCommand{archivePath: path, assertionPath: assertion}
	_, err = cmd.preflight(cmdtesting.Context(c))
	c.Check(err, tc.ErrorMatches, `reading backup assertion ".*": cannot parse backup assertion: .*`)
}

// TestInvalidAssertionFailsBeforeStoreAccess verifies that an invalid
// assertion stops Run in preflight, before the client store is
// consulted or anything is provisioned.
func (s *recoverySuite) TestInvalidAssertionFailsBeforeStoreAccess(c *tc.C) {
	path, _ := writeRecoveryArchive(c, recoveryArchiveFiles(jujuversion.Current.String()))
	assertion := writeRecoveryAssertion(c, path, "deadbeef")
	cmd := &recoveryCommand{archivePath: path, assertionPath: assertion}

	// No client store is configured: Run must fail in preflight.
	err := cmd.Run(cmdtesting.Context(c))
	c.Check(err, tc.ErrorMatches, `reading backup assertion ".*": backup assertion checksum must be a SHA-256 checksum: 64 hexadecimal characters`)
}

// TestPreflightResolvesRelativePaths verifies that relative archive and
// assertion paths resolve against the command context, including files
// in different directories with unrelated basenames.
func (s *recoverySuite) TestPreflightResolvesRelativePaths(c *tc.C) {
	path, sum := writeRecoveryArchive(c, recoveryArchiveFiles(jujuversion.Current.String()))
	assertion := writeRecoveryAssertion(c, path, sum)

	dir := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(dir, "archives"), 0o755), tc.ErrorIsNil)
	c.Assert(os.MkdirAll(filepath.Join(dir, "metadata"), 0o755), tc.ErrorIsNil)
	archivePath := filepath.Join("archives", "renamed.tgz")
	assertionPath := filepath.Join("metadata", "record.yaml")
	c.Assert(os.Rename(path, filepath.Join(dir, archivePath)), tc.ErrorIsNil)
	c.Assert(os.Rename(assertion, filepath.Join(dir, assertionPath)), tc.ErrorIsNil)

	cmd := &recoveryCommand{archivePath: archivePath, assertionPath: assertionPath}
	info, err := cmd.preflight(cmdtesting.ContextForDir(c, dir))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(info.ControllerUUID, tc.Equals, "ctrl-uuid")
}
