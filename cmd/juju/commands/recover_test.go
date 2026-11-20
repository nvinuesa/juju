// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/juju/tc"
	jujusshtools "github.com/juju/utils/v4/ssh"

	"github.com/juju/juju/api/jujuclient"
	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/core/arch"
	corebackups "github.com/juju/juju/core/backups"
	"github.com/juju/juju/core/base"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/core/instance"
	coreos "github.com/juju/juju/core/os"
	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/core/version"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/bootstrap"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/sync"
	"github.com/juju/juju/internal/errors"
	coretesting "github.com/juju/juju/internal/testing"
	osenv "github.com/juju/juju/juju/osenv"
)

type recoverySuite struct {
	coretesting.FakeJujuXDGDataHomeSuite
}

func (s *recoverySuite) SetUpTest(c *tc.C) {
	s.FakeJujuXDGDataHomeSuite.SetUpTest(c)
}

func (s *recoverySuite) TearDownTest(c *tc.C) {
	s.FakeJujuXDGDataHomeSuite.TearDownTest(c)
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
		terminalError = "controller recovery database import is not available in this build; " +
			"started replacement agent on instance test-instance"
	)
	newProvisionedCommand := func() cmd.Command {
		return newRecoveryCommandWithProvisioner(func(
			_ *cmd.Context, _ jujuclient.ClientStore, _ *recovery.ArchiveInfo,
		) error {
			return errors.New(terminalError)
		})
	}

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
		summary:   "valid archive reaches the terminal error",
		assertion: &assertionYAML,
		archive:   true,
		expect: func(_, _ string) string {
			return terminalError
		},
	}, {
		summary:   "assertion records the checksum in uppercase hex",
		assertion: &uppercaseAssertion,
		archive:   true,
		expect: func(_, _ string) string {
			return terminalError
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

		newCmd := newProvisionedCommand
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

// TestRunPassesArchiveInfoToProvisioner checks the handoff after validation.
func (s *recoverySuite) TestRunPassesArchiveInfoToProvisioner(c *tc.C) {
	archive := recoveryArchive(c)
	sum := sha256.Sum256(archive)
	checksum := corebackups.ChecksumSHA256(hex.EncodeToString(sum[:]))
	assertion := "checksum: " + checksum +
		"\nchecksum-format: " + corebackups.ChecksumFormatSHA256 + "\n"

	dir := c.MkDir()
	c.Assert(os.WriteFile(filepath.Join(dir, "backup.tar.gz"), archive, 0o600), tc.ErrorIsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, "backup.assert"), []byte(assertion), 0o600), tc.ErrorIsNil)

	terminalErr := errors.New("recovery stopped after allocation")
	var got *recovery.ArchiveInfo
	command := newRecoveryCommandWithProvisioner(func(
		_ *cmd.Context, _ jujuclient.ClientStore, info *recovery.ArchiveInfo,
	) error {
		got = info
		return terminalErr
	})
	_, err := cmdtesting.RunCommandInDir(
		c, command, []string{"backup.tar.gz", "backup.assert"}, dir,
	)
	c.Assert(err, tc.ErrorIs, terminalErr)
	c.Assert(got, tc.NotNil)
	c.Check(got.ControllerUUID, tc.Equals, "c0ffee00-1111-2222-3333-444455556666")
	c.Check(got.ControllerName, tc.Equals, "source-ctrl")
	c.Check(got.ControllerModelUUID, tc.Equals, "c0ffee00-aaaa-2222-3333-444455556666")
	c.Check(got.CACert, tc.Equals, "source-ca-cert")
	c.Check(got.AgentVersion.String(), tc.Equals, "4.1.0")
	c.Check(got.SourceBase, tc.Equals, "ubuntu@24.04/stable")
	c.Check(got.SourceArch, tc.Equals, "amd64")
	c.Check(got.MachineName, tc.Equals, "0")
	c.Check(got.CloudName, tc.Equals, "lxd")
	c.Check(got.CloudType, tc.Equals, "lxd")
}

func (s *recoverySuite) TestRunPassesKubernetesArchiveInfoToProvisioner(c *tc.C) {
	archive := recoveryArchiveForControllerModel(c, "myk8s", "kubernetes", "caas")
	sum := sha256.Sum256(archive)
	checksum := corebackups.ChecksumSHA256(hex.EncodeToString(sum[:]))
	assertion := "checksum: " + checksum +
		"\nchecksum-format: " + corebackups.ChecksumFormatSHA256 + "\n"

	dir := c.MkDir()
	c.Assert(os.WriteFile(filepath.Join(dir, "backup.tar.gz"), archive, 0o600), tc.ErrorIsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, "backup.assert"), []byte(assertion), 0o600), tc.ErrorIsNil)

	terminalErr := errors.New("recovery stopped after allocation")
	var got *recovery.ArchiveInfo
	command := newRecoveryCommandWithProvisioner(func(
		_ *cmd.Context, _ jujuclient.ClientStore, info *recovery.ArchiveInfo,
	) error {
		got = info
		return terminalErr
	})
	_, err := cmdtesting.RunCommandInDir(
		c, command, []string{"backup.tar.gz", "backup.assert"}, dir,
	)
	c.Assert(err, tc.ErrorIs, terminalErr)
	c.Assert(got, tc.NotNil)
	c.Check(got.ControllerUUID, tc.Equals, "c0ffee00-1111-2222-3333-444455556666")
	c.Check(got.ControllerName, tc.Equals, "source-ctrl")
	c.Check(got.CloudName, tc.Equals, "myk8s")
	c.Check(got.CloudType, tc.Equals, "kubernetes")
}

// TestRecoveryConfigs checks that the replacement controller and
// controller-model configuration are built from the archived identity,
// CA material and agent version, with the controller model named after
// the bootstrap controller model.
func (s *recoverySuite) TestRecoveryConfigs(c *tc.C) {
	info := &recovery.ArchiveInfo{
		ControllerUUID:      "c0ffee00-1111-2222-3333-444455556666",
		ControllerName:      "source-ctrl",
		ControllerModelUUID: "c0ffee00-aaaa-2222-3333-444455556666",
		CACert:              coretesting.CACert,
		AgentVersion:        semversion.MustParse("4.1.0"),
		CloudType:           "lxd",
	}
	controllerConfig, modelConfig, err := recoveryConfigs(info)
	c.Assert(err, tc.ErrorIsNil)

	c.Check(controllerConfig.ControllerUUID(), tc.Equals, info.ControllerUUID)
	caCert, ok := controllerConfig.CACert()
	c.Check(ok, tc.IsTrue)
	c.Check(caCert, tc.Equals, coretesting.CACert)
	c.Check(controllerConfig.ControllerName(), tc.Equals, info.ControllerName)

	c.Check(modelConfig.Name(), tc.Equals, bootstrap.ControllerModelName)
	c.Check(modelConfig.Type(), tc.Equals, info.CloudType)
	c.Check(modelConfig.UUID(), tc.Equals, info.ControllerModelUUID)
	agentVersion, ok := modelConfig.AgentVersion()
	c.Check(ok, tc.IsTrue)
	c.Check(agentVersion, tc.Equals, info.AgentVersion)
}

// TestProvisionRecoveryRequiresSourceBase checks that a machine archive
// which does not record a usable controller base is rejected before any
// cloud access.
func (s *recoverySuite) TestProvisionRecoveryRequiresSourceBase(c *tc.C) {
	for _, sourceBase := range []string{"", "not-a-base"} {
		err := provisionRecovery(cmdtesting.Context(c), nil, &recovery.ArchiveInfo{
			CloudType:    "lxd",
			AgentVersion: version.Current,
			SourceBase:   sourceBase,
			SourceArch:   "amd64",
		})
		c.Check(err, tc.ErrorMatches, "archive does not record a usable controller base.*")
	}
}

// TestProvisionRecoveryRequiresSourceArch checks that an archive which does
// not record a usable controller architecture — empty, or a value no Juju
// release supports — is rejected before any cloud access, so recovery never
// provisions from the bootstrap client's host architecture.
func (s *recoverySuite) TestProvisionRecoveryRequiresSourceArch(c *tc.C) {
	for _, sourceArch := range []string{"", "not-an-arch"} {
		err := provisionRecovery(cmdtesting.Context(c), nil, &recovery.ArchiveInfo{
			CloudType:    "lxd",
			AgentVersion: version.Current,
			SourceBase:   "ubuntu@24.04",
			SourceArch:   sourceArch,
		})
		c.Check(err, tc.ErrorMatches, `archive does not record a usable controller architecture: ".*"`)
	}
}

// The recovery client must request the archived platform even when its
// own architecture differs from the controller's.
func (s *recoverySuite) TestAllocationTargetFromArchive(c *tc.C) {
	nonClientArch := "arm64"
	if arch.HostArch() == "arm64" {
		nonClientArch = "amd64"
	}
	info := &recovery.ArchiveInfo{
		AgentVersion: version.Current,
		SourceBase:   "ubuntu@24.04/stable",
		SourceArch:   nonClientArch,
	}

	params, err := recoveryParamsFromArchive(info)
	c.Assert(err, tc.ErrorIsNil)
	wantBase, err := base.ParseBaseFromString("ubuntu@24.04/stable")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(params.BootstrapBase, tc.Equals, wantBase)
	c.Check(params.SupportedBootstrapBases, tc.DeepEquals, []base.Base{wantBase})
	c.Assert(params.BootstrapConstraints.Arch, tc.NotNil)
	c.Check(*params.BootstrapConstraints.Arch, tc.Equals, nonClientArch)
	c.Assert(params.AvailableTools, tc.HasLen, 1)
	c.Check(params.AvailableTools[0].Version, tc.DeepEquals, semversion.Binary{
		Number: info.AgentVersion, Release: "ubuntu", Arch: nonClientArch,
	})
}

// TestProvisionRecoveryRequiresKubernetesControllerName checks that a
// Kubernetes archive which does not record the source controller name
// is rejected before any cloud access.
func (s *recoverySuite) TestProvisionRecoveryRequiresKubernetesControllerName(c *tc.C) {
	err := provisionRecovery(cmdtesting.Context(c), nil, &recovery.ArchiveInfo{
		CloudType: "kubernetes",
	})
	c.Check(err, tc.ErrorMatches, "recovery archive does not record the source controller name")
}

// TestProvisionRecoveryRequiresMatchingAgentVersion checks that a machine
// archive written by another agent version is rejected before any cloud
// access, since the replacement runs a locally built agent.
func (s *recoverySuite) TestProvisionRecoveryRequiresMatchingAgentVersion(c *tc.C) {
	other := version.Current
	other.Patch++
	err := provisionRecovery(cmdtesting.Context(c), nil, &recovery.ArchiveInfo{
		CloudType:    "lxd",
		AgentVersion: other,
		SourceBase:   "ubuntu@24.04",
		MachineName:  "0",
	})
	c.Check(err, tc.ErrorMatches, "archive was created by agent version .* recovery requires the exact same version")
}

// TestProvisionRecoveryRequiresIAASMachineName checks that a machine
// archive which does not record the source controller machine is rejected
// before any cloud access.
func (s *recoverySuite) TestProvisionRecoveryRequiresIAASMachineName(c *tc.C) {
	err := provisionRecovery(cmdtesting.Context(c), nil, &recovery.ArchiveInfo{
		CloudType:    "lxd",
		AgentVersion: version.Current,
		SourceBase:   "ubuntu@24.04",
		SourceArch:   "amd64",
	})
	c.Check(err, tc.ErrorMatches, "archive does not record a usable controller machine")
}

// TestCheckAgentPlatform checks that the packaged agent is accepted only
// when the client host base and architecture match the archived controller
// base and architecture: the packaged agent runs on the client platform, so
// a series or architecture mismatch is refused.
func (s *recoverySuite) TestCheckAgentPlatform(c *tc.C) {
	jammy := base.MustParseBaseFromString("ubuntu@22.04")
	noble := base.MustParseBaseFromString("ubuntu@24.04")
	s.PatchValue(&coreos.HostBase, func() (base.Base, error) {
		return noble, nil
	})

	archivedVersion := version.Current
	params := bootstrap.IAASRecoveryParams{
		BootstrapParams: bootstrap.BootstrapParams{
			AgentVersion:         &archivedVersion,
			BootstrapBase:        jammy,
			BootstrapConstraints: constraints.MustParse("arch=amd64"),
		},
	}
	builtAgent := &sync.BuiltAgent{
		Version: semversion.Binary{
			Number:  archivedVersion,
			Release: "ubuntu",
			Arch:    arch.AMD64,
		},
	}

	// A noble client host cannot package an agent for a jammy replacement.
	err := checkAgentPlatform(builtAgent, params)
	c.Check(err, tc.ErrorMatches, regexp.QuoteMeta(
		"replacement requires base \""+jammy.String()+"\" and arch \"amd64\", "+
			"but the client host is base \""+noble.String()+"\" and arch \"amd64\""))

	// An architecture mismatch is refused even when the base matches.
	params.BootstrapBase = noble
	builtAgent.Version.Arch = arch.ARM64
	err = checkAgentPlatform(builtAgent, params)
	c.Check(err, tc.ErrorMatches, regexp.QuoteMeta(
		"replacement requires base \""+noble.String()+"\" and arch \"amd64\", "+
			"but the client host is base \""+noble.String()+"\" and arch \"arm64\""))

	// A matching base and architecture is accepted.
	builtAgent.Version.Arch = arch.AMD64
	c.Check(checkAgentPlatform(builtAgent, params), tc.ErrorIsNil)
}

// TestProvisionRecoveryFinalizesKubernetesCloudHostRegion checks that
// FinalizeCloud runs before the host-cloud-region guard so built-in clouds
// such as microk8s (whose HostCloudRegion is discovered during finalization)
// are accepted, while clouds that remain without a host cloud region after
// finalization are still rejected.
func (s *recoverySuite) TestProvisionRecoveryFinalizesKubernetesCloudHostRegion(c *tc.C) {
	c.Assert(jujucloud.WritePersonalCloudMetadata(map[string]jujucloud.Cloud{
		"myk8s": {
			Name:    "myk8s",
			Type:    jujucloud.CloudTypeKubernetes,
			Regions: []jujucloud.Region{{Name: "localhost"}},
		},
	}), tc.ErrorIsNil)

	var hostRegion string
	s.PatchValue(&lookupRecoveryProvider, func(string) (environs.EnvironProvider, error) {
		return fakeK8sRecoveryProvider{hostCloudRegion: &hostRegion}, nil
	})

	info := &recovery.ArchiveInfo{
		ControllerName: "source-ctrl",
		CloudName:      "myk8s",
		CloudType:      jujucloud.CloudTypeKubernetes,
	}
	err := provisionRecovery(cmdtesting.Context(c), jujuclient.NewMemStore(), info)
	c.Check(err, tc.ErrorMatches, "recovery cloud does not record a host cloud region.*")

	hostRegion = "microk8s/localhost"
	err = provisionRecovery(cmdtesting.Context(c), jujuclient.NewMemStore(), info)
	c.Check(err, tc.ErrorMatches, ".*no credentials.*|.*credentials.*")
}

type fakeK8sRecoveryProvider struct {
	noCredentialsProvider
	hostCloudRegion *string
}

func (p fakeK8sRecoveryProvider) FinalizeCloud(_ environs.FinalizeCloudContext, cld jujucloud.Cloud) (jujucloud.Cloud, error) {
	cld.HostCloudRegion = *p.hostCloudRegion
	return cld, nil
}

// oneCredentialProvider detects exactly one credential so the machine
// recovery provisioning tests can pass cloud-credential resolution without
// a populated client store.
type oneCredentialProvider struct {
	environs.CloudEnvironProvider
}

func (oneCredentialProvider) DetectCredentials(string) (*jujucloud.CloudCredential, error) {
	return &jujucloud.CloudCredential{
		DefaultRegion: "localhost",
		AuthCredentials: map[string]jujucloud.Credential{
			"recovery-cred": jujucloud.NewCredential("one", nil),
		},
	}, nil
}

func (oneCredentialProvider) CredentialSchemas() map[jujucloud.AuthType]jujucloud.CredentialSchema {
	return map[jujucloud.AuthType]jujucloud.CredentialSchema{"one": {}}
}

func (oneCredentialProvider) FinalizeCredential(
	_ environs.FinalizeCredentialContext, fp environs.FinalizeCredentialParams,
) (*jujucloud.Credential, error) {
	credential := fp.Credential
	return &credential, nil
}

// fakeRecoveryEnviron stands in for the opened controller environ so a
// test can reach the machine agent installation block without a provider
// registry entry.
type fakeRecoveryEnviron struct {
	environs.BootstrapEnviron

	preparedFor string
}

func (e *fakeRecoveryEnviron) PrepareForBootstrap(
	_ environs.BootstrapContext, controllerName string,
) error {
	e.preparedFor = controllerName
	return nil
}

// iaasRecoveryFakes bundles the fakes installed for the machine recovery
// provisioning tests.
type iaasRecoveryFakes struct {
	environ    *fakeRecoveryEnviron
	info       *recovery.ArchiveInfo
	openParams bootstrap.PrepareParams
}

// patchIAASRecovery installs the cloud metadata, provider and controller
// fakes that let provisionRecovery resolve the archived cloud and open a
// fake controller environ, so a test can reach and inspect the machine
// agent installation block.
func (s *recoverySuite) patchIAASRecovery(c *tc.C) *iaasRecoveryFakes {
	c.Assert(jujucloud.WritePersonalCloudMetadata(map[string]jujucloud.Cloud{
		"recovery-lxd": {
			Name:    "recovery-lxd",
			Type:    "lxd",
			Regions: []jujucloud.Region{{Name: "localhost"}},
		},
	}), tc.ErrorIsNil)

	fakes := &iaasRecoveryFakes{
		environ: &fakeRecoveryEnviron{},
		info: &recovery.ArchiveInfo{
			ControllerUUID:      "c0ffee00-1111-2222-3333-444455556666",
			ControllerName:      "recovered",
			ControllerModelUUID: "c0ffee00-aaaa-2222-3333-444455556666",
			CACert:              coretesting.CACert,
			CAPrivateKey:        coretesting.CAKey,
			AgentVersion:        version.Current,
			SourceBase:          "ubuntu@24.04",
			SourceArch:          "amd64",
			MachineName:         "0",
			CloudName:           "recovery-lxd",
			CloudType:           "lxd",
		},
	}
	s.PatchValue(&lookupRecoveryProvider, func(string) (environs.EnvironProvider, error) {
		return oneCredentialProvider{}, nil
	})
	s.PatchValue(&openRecoveryController, func(
		_ bool, _ environs.BootstrapContext, params bootstrap.PrepareParams,
	) (*bootstrap.ControllerPreparation, error) {
		fakes.openParams = params
		return &bootstrap.ControllerPreparation{Environ: fakes.environ}, nil
	})
	return fakes
}

// TestProvisionRecoveryRequiresSSHKeys checks that a machine recovery with
// no client SSH keys is refused after the controller environ is prepared,
// before any agent is packaged or instance allocated.
func (s *recoverySuite) TestProvisionRecoveryRequiresSSHKeys(c *tc.C) {
	fakes := s.patchIAASRecovery(c)
	jujusshtools.ClearClientKeys()

	err := provisionRecovery(cmdtesting.Context(c), jujuclient.NewMemStore(), fakes.info)
	c.Check(err, tc.ErrorMatches, "no Juju SSH keys available for recovery")
	c.Check(fakes.environ.preparedFor, tc.Equals, "recovered")
}

// TestProvisionRecoveryAgentBuildFailure checks that a failure to package
// the agent tarball is reported before any cloud allocation happens.
func (s *recoverySuite) TestProvisionRecoveryAgentBuildFailure(c *tc.C) {
	fakes := s.patchIAASRecovery(c)
	c.Assert(jujusshtools.LoadClientKeys(osenv.JujuXDGDataHomePath("ssh")), tc.ErrorIsNil)
	s.PatchValue(&sync.BuildAgentTarball, func(
		bool, string, func(semversion.Number) semversion.Number,
	) (*sync.BuiltAgent, error) {
		return nil, errors.New("agent build failed")
	})

	err := provisionRecovery(cmdtesting.Context(c), jujuclient.NewMemStore(), fakes.info)
	c.Check(err, tc.ErrorMatches, "agent build failed")
}

// TestProvisionRecoveryRejectsPlatformMismatch checks that the packaged
// agent is refused when its architecture cannot run on the archived
// controller machine.
func (s *recoverySuite) TestProvisionRecoveryRejectsPlatformMismatch(c *tc.C) {
	fakes := s.patchIAASRecovery(c)
	c.Assert(jujusshtools.LoadClientKeys(osenv.JujuXDGDataHomePath("ssh")), tc.ErrorIsNil)
	noble := base.MustParseBaseFromString("ubuntu@24.04")
	s.PatchValue(&coreos.HostBase, func() (base.Base, error) {
		return noble, nil
	})
	s.PatchValue(&sync.BuildAgentTarball, func(
		bool, string, func(semversion.Number) semversion.Number,
	) (*sync.BuiltAgent, error) {
		return &sync.BuiltAgent{
			Version: semversion.Binary{
				Number: version.Current, Release: "ubuntu", Arch: arch.ARM64,
			},
		}, nil
	})

	err := provisionRecovery(cmdtesting.Context(c), jujuclient.NewMemStore(), fakes.info)
	c.Check(err, tc.ErrorMatches, regexp.QuoteMeta(
		"replacement requires base \""+noble.String()+"\" and arch \"amd64\", "+
			"but the client host is base \""+noble.String()+"\" and arch \"arm64\""))
}

// TestProvisionRecoveryPreparesIAASRecovery checks the machine agent
// installation block end to end: the packaged agent tarball, the archived
// identity and the client SSH keys are wired into the recovery parameters,
// and the terminal message reports the started replacement agent.
func (s *recoverySuite) TestProvisionRecoveryPreparesIAASRecovery(c *tc.C) {
	fakes := s.patchIAASRecovery(c)
	c.Assert(jujusshtools.LoadClientKeys(osenv.JujuXDGDataHomePath("ssh")), tc.ErrorIsNil)
	noble := base.MustParseBaseFromString("ubuntu@24.04")
	s.PatchValue(&coreos.HostBase, func() (base.Base, error) {
		return noble, nil
	})
	builtAgent := &sync.BuiltAgent{
		Version: semversion.Binary{
			Number: version.Current, Release: "ubuntu", Arch: arch.AMD64,
		},
		Dir:         c.MkDir(),
		StorageName: "agents.tar.gz",
		Sha256Hash:  "cafebabe",
		Size:        1234,
	}
	var gotBuild bool
	s.PatchValue(&sync.BuildAgentTarball, func(
		build bool, _ string, _ func(semversion.Number) semversion.Number,
	) (*sync.BuiltAgent, error) {
		gotBuild = build
		return builtAgent, nil
	})
	var gotEnviron environs.BootstrapEnviron
	var gotParams bootstrap.IAASRecoveryParams
	s.PatchValue(&recoverIAAS, func(
		_ environs.BootstrapContext, environ environs.BootstrapEnviron,
		params bootstrap.IAASRecoveryParams,
	) (instance.Id, error) {
		gotEnviron = environ
		gotParams = params
		return "test-instance", nil
	})

	err := provisionRecovery(cmdtesting.Context(c), jujuclient.NewMemStore(), fakes.info)
	c.Check(err, tc.ErrorMatches,
		"controller recovery database import is not available in this build; "+
			"started replacement agent on instance test-instance")

	// The agent tarball is packaged from the client install, never built.
	c.Check(gotBuild, tc.IsFalse)
	c.Check(gotEnviron, tc.Equals, environs.BootstrapEnviron(fakes.environ))
	c.Check(fakes.environ.preparedFor, tc.Equals, "recovered")

	keyFiles := jujusshtools.PrivateKeyFiles()
	sorted := append([]string(nil), keyFiles...)
	sort.Strings(sorted)

	c.Assert(gotParams.AvailableTools, tc.HasLen, 1)
	tool := gotParams.AvailableTools[0]
	c.Check(tool.Version.Number, tc.Equals, version.Current)
	c.Check(tool.Version.Release, tc.Equals, "ubuntu")
	c.Check(tool.Version.Arch, tc.Equals, arch.AMD64)
	c.Check(tool.URL, tc.Equals,
		"file://"+filepath.Join(builtAgent.Dir, builtAgent.StorageName))
	c.Check(tool.SHA256, tc.Equals, builtAgent.Sha256Hash)
	c.Check(tool.Size, tc.Equals, builtAgent.Size)

	c.Check(gotParams.MachineID, tc.Equals, "0")
	c.Check(gotParams.BootstrapBase, tc.Equals, noble)
	c.Check(gotParams.SupportedBootstrapBases, tc.DeepEquals, []base.Base{noble})
	c.Assert(gotParams.BootstrapConstraints.Arch, tc.NotNil)
	c.Check(*gotParams.BootstrapConstraints.Arch, tc.Equals, arch.AMD64)
	c.Check(gotParams.CAPrivateKey, tc.Equals, coretesting.CAKey)
	c.Check(gotParams.ControllerConfig.ControllerUUID(), tc.Equals, fakes.info.ControllerUUID)
	c.Check(gotParams.AdminSecret, tc.Not(tc.Equals), "")
	c.Check(gotParams.Cloud.Name, tc.Equals, "recovery-lxd")
	c.Check(gotParams.CloudRegion, tc.Equals, "localhost")
	c.Check(gotParams.CloudCredentialName, tc.Equals, "recovery-cred")

	c.Check(gotParams.BootstrapSSHAuthorizedKeys, tc.HasLen, len(sorted))
	c.Check(gotParams.DialOpts.IdentityFiles, tc.DeepEquals, sorted)
	c.Check(gotParams.DialOpts.Timeout, tc.Equals, 20*time.Minute)
	c.Check(gotParams.DialOpts.RetryDelay, tc.Equals, 5*time.Second)
	c.Check(gotParams.DialOpts.AddressesDelay, tc.Equals, 5*time.Second)

	// The controller preparation carried the archived identity.
	c.Check(fakes.openParams.ControllerName, tc.Equals, "recovered")
	c.Check(fakes.openParams.Cloud.Name, tc.Equals, "recovery-lxd")
	c.Check(fakes.openParams.Cloud.Region, tc.Equals, "localhost")
	c.Check(fakes.openParams.CredentialName, tc.Equals, "recovery-cred")
	c.Check(fakes.openParams.AdminSecret, tc.Not(tc.Equals), "")
	c.Check(fakes.openParams.ModelConfig[config.UUIDKey], tc.Equals, fakes.info.ControllerModelUUID)
	c.Check(fakes.openParams.ModelConfig[config.TypeKey], tc.Equals, "lxd")
	c.Check(fakes.openParams.ControllerConfig.ControllerUUID(), tc.Equals, fakes.info.ControllerUUID)
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

// recoveryArchive renders the default minimal LXD recovery archive.
func recoveryArchive(c *tc.C) []byte {
	return recoveryArchiveForControllerModel(c, "lxd", "lxd", "iaas")
}

// recoveryArchiveForControllerModel renders a minimal valid recovery
// archive: the provenance metadata, a controller dump recording one
// controller model on the given cloud, that model's database dump, and the
// data-directory files archive. It is the smallest archive the recover
// command's validation accepts, so reaching the terminal error proves the
// whole read and verify path ran.
func recoveryArchiveForControllerModel(
	c *tc.C, cloudName, cloudType, modelType string,
) []byte {
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
		// The base is what the real producer records: the machine
		// service's base string, channel included.
		`"Base":"ubuntu@24.04/stable",` +
		`"Arch":"amd64",` +
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
  - uuid: cloud-source
    name: ` + cloudName + `
    cloud_type_id: 1
  cloud_type:
  - id: 1
    type: ` + cloudType + `
  model_type:
  - id: 0
    type: ` + modelType + `
  controller_config:
  - key: controller-name
    value: source-ctrl
  model:
  - uuid: ` + controllerModelUUID + `
    name: controller
    cloud_uuid: cloud-source
    model_type_id: 0
`
	files := map[string][]byte{
		"juju-backup/metadata.json":                                []byte(metadata),
		"juju-backup/dump/controller.yaml":                         []byte(controllerDump),
		"juju-backup/dump/models/" + controllerModelUUID + ".yaml": []byte("payload: {}\n"),
		"juju-backup/root.tar":                                     []byte("blobs"),
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
