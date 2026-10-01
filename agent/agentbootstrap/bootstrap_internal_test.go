// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agentbootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/juju/clock"
	"github.com/juju/names/v6"
	"github.com/juju/tc"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/controller"
	corelogger "github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/database"
	"github.com/juju/juju/internal/database/app"
	loggertesting "github.com/juju/juju/internal/logger/testing"
)

type bootstrapInternalSuite struct{}

func TestBootstrapInternalSuite(t *testing.T) {
	tc.Run(t, &bootstrapInternalSuite{})
}

func (*bootstrapInternalSuite) TestBootstrapMachineAddressesReachDqlite(c *tc.C) {
	addresses := network.NewMachineAddresses([]string{"10.0.0.1"}).AsProviderAddresses()
	var gotAddresses network.ProviderAddresses
	bootstrap, err := NewAgentBootstrap(AgentBootstrapArgs{
		AdminUser:                 names.NewLocalUserTag("admin"),
		AgentConfig:               stubAgentConfig{dataDir: c.MkDir()},
		BootstrapEnviron:          stubBootstrapEnviron{},
		BootstrapMachineAddresses: addresses,
		BootstrapDqlite: func(
			_ context.Context, manager database.BootstrapNodeManager,
			bootstrapAddresses network.ProviderAddresses, _ model.UUID,
			_ corelogger.Logger,
			_ ...database.BootstrapOpt,
		) error {
			c.Check(manager, tc.NotNil)
			gotAddresses = bootstrapAddresses
			return nil
		},
		Logger: loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)

	err = bootstrap.initializeDqlite(c.Context(), tc.Must0(c, model.NewUUID), nil)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(gotAddresses, tc.DeepEquals, addresses)
}

func (*bootstrapInternalSuite) TestInitializeDqliteRestoreDispatch(c *tc.C) {
	var restoreCalled bool
	var gotStage database.RestoreStage
	bootstrap, err := NewAgentBootstrap(AgentBootstrapArgs{
		AdminUser:        names.NewLocalUserTag("admin"),
		AgentConfig:      stubAgentConfig{dataDir: c.MkDir()},
		BootstrapEnviron: stubBootstrapEnviron{},
		BootstrapMachineAddresses: network.NewMachineAddresses(
			[]string{"10.0.0.1"}).AsProviderAddresses(),
		BootstrapDqlite: func(
			context.Context, database.BootstrapNodeManager,
			network.ProviderAddresses, model.UUID,
			corelogger.Logger, ...database.BootstrapOpt,
		) error {
			return errors.New("must not be called in restore mode")
		},
		BootstrapDqliteRestore: func(
			_ context.Context, _ database.BootstrapNodeManager,
			_ network.ProviderAddresses, _ model.UUID,
			_ corelogger.Logger, stage database.RestoreStage,
			opts ...database.BootstrapOpt,
		) error {
			restoreCalled = true
			gotStage = stage
			c.Check(opts, tc.HasLen, 0)
			return nil
		},
		Logger: loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)

	err = bootstrap.initializeDqlite(c.Context(), tc.Must0(c, model.NewUUID),
		func(context.Context, *app.App) error { return nil })
	c.Assert(err, tc.ErrorIsNil)
	c.Check(restoreCalled, tc.IsTrue)
	c.Check(gotStage, tc.NotNil)
}

func (*bootstrapInternalSuite) TestWaitForRestoreArchivePresent(c *tc.C) {
	path := filepath.Join(c.MkDir(), "archive.tar.gz")
	c.Assert(os.WriteFile(path, []byte("x"), 0o600), tc.ErrorIsNil)

	b := &AgentBootstrap{logger: loggertesting.WrapCheckLog(c), clock: clock.WallClock}
	c.Check(b.waitForRestoreArchive(c.Context(), path), tc.ErrorIsNil)
}

func (*bootstrapInternalSuite) TestWaitForRestoreArchiveCancelled(c *tc.C) {
	ctx, cancel := context.WithCancel(c.Context())
	cancel()

	b := &AgentBootstrap{logger: loggertesting.WrapCheckLog(c), clock: clock.WallClock}
	err := b.waitForRestoreArchive(ctx, filepath.Join(c.MkDir(), "missing.tar.gz"))
	c.Assert(err, tc.NotNil)
}

func (*bootstrapInternalSuite) TestWaitForRestoreArchiveTimeout(c *tc.C) {
	// A lost upload must fail the wait with a clear error instead of
	// hanging until the bootstrap-wide timeout.
	old := restoreArchiveWaitTimeout
	restoreArchiveWaitTimeout = -1 * time.Second
	defer func() { restoreArchiveWaitTimeout = old }()

	b := &AgentBootstrap{logger: loggertesting.WrapCheckLog(c), clock: clock.WallClock}
	err := b.waitForRestoreArchive(c.Context(), filepath.Join(c.MkDir(), "missing.tar.gz"))
	c.Assert(err, tc.ErrorMatches,
		`restore archive did not appear at .*; the upload may have failed and bootstrap must be re-run`)
}

type stubAgentConfig struct {
	agent.ConfigSetter
	dataDir string
}

func (s stubAgentConfig) DataDir() string {
	return s.dataDir
}

func (stubAgentConfig) CACert() string {
	return "ca-cert"
}

func (stubAgentConfig) ControllerAgentInfo() (controller.ControllerAgentInfo, bool) {
	return controller.ControllerAgentInfo{
		Cert:       "controller-cert",
		PrivateKey: "controller-key",
	}, true
}

type stubBootstrapEnviron struct {
	environs.BootstrapEnviron
}
