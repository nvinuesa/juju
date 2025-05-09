// Copyright 2019 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"fmt"

	"github.com/juju/errors"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"go.uber.org/mock/gomock"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/cmd/juju/commands/mocks"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/semversion"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/environs/sync"
	toolstesting "github.com/juju/juju/environs/tools/testing"
	"github.com/juju/juju/internal/cmd"
	"github.com/juju/juju/internal/cmd/cmdtesting"
	coretesting "github.com/juju/juju/internal/testing"
	"github.com/juju/juju/jujuclient"
)

func newUpgradeControllerCommandForTest(
	store jujuclient.ClientStore,
	modelConfigAPI ModelConfigAPI,
	modelUpgrader ModelUpgraderAPI,
	options ...modelcmd.WrapControllerOption,
) cmd.Command {
	command := &upgradeControllerCommand{
		modelConfigAPI:   modelConfigAPI,
		modelUpgraderAPI: modelUpgrader,
	}
	command.SetClientStore(store)
	return modelcmd.WrapController(command, options...)
}

type upgradeControllerSuite struct {
	testing.IsolationSuite

	modelConfigAPI *mocks.MockModelConfigAPI
	modelUpgrader  *mocks.MockModelUpgraderAPI
	store          *mocks.MockClientStore
}

var _ = gc.Suite(&upgradeControllerSuite{})

func (s *upgradeControllerSuite) upgradeControllerCommand(c *gc.C, isCAAS bool) (*gomock.Controller, cmd.Command) {
	ctrl := gomock.NewController(c)
	s.modelConfigAPI = mocks.NewMockModelConfigAPI(ctrl)
	s.modelUpgrader = mocks.NewMockModelUpgraderAPI(ctrl)
	s.store = mocks.NewMockClientStore(ctrl)

	s.modelConfigAPI.EXPECT().Close().AnyTimes()
	s.modelUpgrader.EXPECT().Close().AnyTimes()

	s.store.EXPECT().CurrentController().AnyTimes().Return("c-1", nil)
	s.store.EXPECT().ControllerByName("c-1").AnyTimes().Return(&jujuclient.ControllerDetails{
		APIEndpoints: []string{"0.1.2.3:1234"},
	}, nil)
	s.store.EXPECT().CurrentModel("c-1").AnyTimes().Return("m-1", nil)
	s.store.EXPECT().AccountDetails("c-1").AnyTimes().Return(&jujuclient.AccountDetails{User: "foo", LastKnownAccess: "superuser"}, nil)
	cookieJar := mocks.NewMockCookieJar(ctrl)
	cookieJar.EXPECT().Save().AnyTimes().Return(nil)
	s.store.EXPECT().CookieJar("c-1").AnyTimes().Return(cookieJar, nil)

	modelType := model.IAAS
	if isCAAS {
		modelType = model.CAAS
	}

	s.store.EXPECT().ModelByName("c-1", "admin/controller").AnyTimes().Return(&jujuclient.ModelDetails{
		ModelUUID: coretesting.ModelTag.Id(),
		ModelType: modelType,
	}, nil)

	return ctrl, newUpgradeControllerCommandForTest(s.store,
		s.modelConfigAPI, s.modelUpgrader,
	)
}

func (s *upgradeControllerSuite) TestUpgradeModelFailedCAASWithBuildAgent(c *gc.C) {
	ctrl, cmd := s.upgradeControllerCommand(c, true)
	defer ctrl.Finish()

	_, err := cmdtesting.RunCommand(c, cmd, `--build-agent`)
	c.Assert(err, gc.ErrorMatches, `--build-agent for k8s model upgrades not supported`)
}

func (s *upgradeControllerSuite) TestUpgradeModelProvidedAgentVersionUpToDate(c *gc.C) {
	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"agent-version": coretesting.FakeVersionNumber.String(),
	})

	s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil)

	ctx, err := cmdtesting.RunCommand(c, cmd, "--agent-version", coretesting.FakeVersionNumber.String())
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "no upgrades available\n")
}

func (s *upgradeControllerSuite) TestUpgradeModelFailedWithBuildAgentAndAgentVersion(c *gc.C) {
	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"agent-version": coretesting.FakeVersionNumber.String(),
	})

	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
	)

	_, err := cmdtesting.RunCommand(c, cmd,
		"--build-agent",
		"--agent-version", semversion.MustParse("3.9.99").String(),
	)
	c.Assert(err, gc.ErrorMatches, `--build-agent cannot be used with --agent-version together`)
}

func (s *upgradeControllerSuite) TestUpgradeModelWithAgentVersion(c *gc.C) {
	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		// TODO (hml) 19-oct-2022
		// Once upgrade from 2.9 to 3.0 is supported, go back to
		// using coretesting.FakeVersionNumber.String() in this
		// test.
		//"agent-version": coretesting.FakeVersionNumber.String(),
		"agent-version": "3.0.1",
	})

	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), semversion.MustParse("3.9.99"),
			"", false, false,
		).Return(semversion.MustParse("3.9.99"), nil),
	)

	ctx, err := cmdtesting.RunCommand(c, cmd,
		"--agent-version", semversion.MustParse("3.9.99").String(),
	)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, `
best version:
    3.9.99
`[1:])
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, `
started upgrade to 3.9.99
`[1:])
}

func (s *upgradeControllerSuite) TestUpgradeModelWithAgentVersionUploadLocalOfficial(c *gc.C) {
	s.reset(c)

	s.PatchValue(&jujuversion.Current, func() semversion.Number {
		v := jujuversion.Current
		v.Build = 0
		return v
	}())

	s.PatchValue(&CheckCanImplicitUpload,
		func(model.ModelType, bool, semversion.Number, semversion.Number) bool { return true },
	)

	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	agentVersion := coretesting.FakeVersionNumber
	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"agent-version": agentVersion.String(),
	})

	c.Assert(agentVersion.Build, gc.Equals, 0)
	builtVersion := coretesting.CurrentVersion()
	targetVersion := builtVersion.Number
	builtVersion.Build++
	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), targetVersion,
			"", false, false,
		).Return(
			semversion.Zero,
			errors.NotFoundf("available agent tool, upload required"),
		),
		s.modelUpgrader.EXPECT().UploadTools(gomock.Any(), gomock.Any(), builtVersion).Return(nil, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), builtVersion.Number,
			"", false, false,
		).Return(builtVersion.Number, nil),
	)

	ctx, err := cmdtesting.RunCommand(c, cmd,
		"--agent-version", targetVersion.String(),
	)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, fmt.Sprintf(`
best version:
    %s
`, builtVersion.Number)[1:])
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, fmt.Sprintf(`
no prepackaged agent binaries available, using the local snap jujud %s
started upgrade to %s
`, builtVersion.Number, builtVersion.Number)[1:])
}

func (s *upgradeControllerSuite) TestUpgradeModelWithAgentVersionAlreadyUpToDate(c *gc.C) {
	s.reset(c)

	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	agentVersion := coretesting.FakeVersionNumber
	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"agent-version": agentVersion.String(),
	})

	c.Assert(agentVersion.Build, gc.Equals, 0)
	targetVersion := coretesting.CurrentVersion()
	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), targetVersion.ToPatch(),
			"", false, false,
		).Return(
			semversion.Zero,
			errors.AlreadyExistsf("up to date"),
		),
	)

	ctx, err := cmdtesting.RunCommand(c, cmd,
		"--agent-version", targetVersion.ToPatch().String(),
	)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "no upgrades available\n")
}

func (s *upgradeControllerSuite) TestUpgradeModelWithAgentVersionFailedExpectUploadButWrongTargetVersion(c *gc.C) {
	s.reset(c)

	s.PatchValue(&CheckCanImplicitUpload,
		func(model.ModelType, bool, semversion.Number, semversion.Number) bool { return true },
	)

	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	agentVersion := coretesting.FakeVersionNumber
	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"agent-version": agentVersion.String(),
	})

	current := agentVersion
	current.Minor++ // local snap is newer.
	s.PatchValue(&jujuversion.Current, current)

	targetVersion := current
	targetVersion.Patch++ // wrong target version (It has to be equal to local snap version).
	c.Assert(targetVersion.Compare(current) == 0, jc.IsFalse)

	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), targetVersion,
			"", false, false,
		).Return(
			semversion.Zero,
			errors.NotFoundf("available agent tool, upload required"),
		),
	)

	ctx, err := cmdtesting.RunCommand(c, cmd,
		"--agent-version", targetVersion.String(),
	)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "no upgrades available\n")
}

func (s *upgradeControllerSuite) TestUpgradeModelWithAgentVersionExpectUploadFailedDueToNotAllowed(c *gc.C) {
	s.reset(c)

	s.PatchValue(&CheckCanImplicitUpload,
		func(model.ModelType, bool, semversion.Number, semversion.Number) bool { return false },
	)

	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	agentVersion := coretesting.FakeVersionNumber
	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"agent-version": agentVersion.String(),
	})

	targetVersion := coretesting.CurrentVersion().Number
	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), targetVersion,
			"", false, false,
		).Return(
			semversion.Zero,
			errors.NotFoundf("available agent tool, upload required"),
		),
	)

	ctx, err := cmdtesting.RunCommand(c, cmd,
		"--agent-version", targetVersion.String(),
	)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "no upgrades available\n")
}

func (s *upgradeControllerSuite) TestUpgradeModelWithAgentVersionDryRun(c *gc.C) {
	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		// TODO (hml) 19-oct-2022
		// Once upgrade from 2.9 to 3.0 is supported, go back to
		// using coretesting.FakeVersionNumber.String() in this
		// test.
		//"agent-version": coretesting.FakeVersionNumber.String(),
		"agent-version": "3.0.1",
	})

	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), semversion.MustParse("3.9.99"),
			"", false, true,
		).Return(semversion.MustParse("3.9.99"), nil),
	)

	ctx, err := cmdtesting.RunCommand(c, cmd,
		"--agent-version", semversion.MustParse("3.9.99").String(), "--dry-run",
	)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, `
best version:
    3.9.99
upgrade to this version by running
    juju upgrade-controller
`[1:])
}

func (s *upgradeControllerSuite) TestUpgradeModelWithAgentVersionGotBlockers(c *gc.C) {
	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		// TODO (hml) 19-oct-2022
		// Once upgrade from 2.9 to 3.0 is supported, go back to
		// using coretesting.FakeVersionNumber.String() in this
		// test.
		//"agent-version": coretesting.FakeVersionNumber.String(),
		"agent-version": "3.0.1",
	})

	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), semversion.MustParse("3.9.99"),
			"", false, false,
		).Return(semversion.Zero, errors.New(`
cannot upgrade to "3.9.99" due to issues with these models:
"admin/default":
- the model hosts deprecated ubuntu machine(s): bionic(3) (not supported)
`[1:])),
	)

	_, err := cmdtesting.RunCommand(c, cmd,
		"--agent-version", semversion.MustParse("3.9.99").String(),
	)
	c.Assert(err.Error(), gc.Equals, `
cannot upgrade to "3.9.99" due to issues with these models:
"admin/default":
- the model hosts deprecated ubuntu machine(s): bionic(3) (not supported)
`[1:])
}

func (s *upgradeControllerSuite) reset(c *gc.C) {
	s.PatchValue(&sync.BuildAgentTarball, toolstesting.GetMockBuildTools(c))
}

func (s *upgradeControllerSuite) TestUpgradeModelWithBuildAgent(c *gc.C) {
	s.reset(c)

	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	agentVersion := coretesting.FakeVersionNumber
	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"agent-version": agentVersion.String(),
	})
	c.Assert(agentVersion.Build, gc.Equals, 0)
	builtVersion := coretesting.CurrentVersion()
	builtVersion.Build++
	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UploadTools(gomock.Any(), gomock.Any(), builtVersion).Return(nil, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), builtVersion.Number,
			"", false, false,
		).Return(builtVersion.Number, nil),
	)

	ctx, err := cmdtesting.RunCommand(c, cmd, "--build-agent")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, fmt.Sprintf(`
best version:
    %s
`, builtVersion.Number)[1:])
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, fmt.Sprintf(`
no prepackaged agent binaries available, using local agent binary %s (built from source)
started upgrade to %s
`, builtVersion.Number, builtVersion.Number)[1:])
}

func (s *upgradeControllerSuite) TestUpgradeModelUpToDate(c *gc.C) {
	s.reset(c)

	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	agentVersion := coretesting.FakeVersionNumber
	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"agent-version": agentVersion.String(),
	})

	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), semversion.Zero,
			"", false, false,
		).Return(semversion.Zero, errors.AlreadyExistsf("up to date")),
	)

	ctx, err := cmdtesting.RunCommand(c, cmd)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, "no upgrades available\n")
}

func (s *upgradeControllerSuite) TestUpgradeModelUpgradeToPublishedVersion(c *gc.C) {
	s.reset(c)

	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	agentVersion := coretesting.FakeVersionNumber
	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"agent-version": agentVersion.String(),
	})

	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), semversion.Zero,
			"", false, false,
		).Return(semversion.MustParse("3.9.99"), nil),
	)

	ctx, err := cmdtesting.RunCommand(c, cmd)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, `
best version:
    3.9.99
`[1:])
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, `
started upgrade to 3.9.99
`[1:])
}

func (s *upgradeControllerSuite) TestUpgradeModelWithStream(c *gc.C) {
	s.reset(c)

	ctrl, cmd := s.upgradeControllerCommand(c, false)
	defer ctrl.Finish()

	agentVersion := coretesting.FakeVersionNumber
	cfg := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"agent-version": agentVersion.String(),
	})

	gomock.InOrder(
		s.modelConfigAPI.EXPECT().ModelGet(gomock.Any()).Return(cfg, nil),
		s.modelUpgrader.EXPECT().UpgradeModel(
			gomock.Any(),
			coretesting.ModelTag.Id(), semversion.Zero,
			"proposed", false, false,
		).Return(semversion.MustParse("3.9.99"), nil),
	)

	ctx, err := cmdtesting.RunCommand(c, cmd, "--agent-stream", "proposed")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(cmdtesting.Stderr(ctx), gc.Equals, `
best version:
    3.9.99
`[1:])
	c.Assert(cmdtesting.Stdout(ctx), gc.Equals, `
started upgrade to 3.9.99
`[1:])
}

func (s *upgradeControllerSuite) TestCheckCanImplicitUploadIAASModel(c *gc.C) {
	ctrl := gomock.NewController(c)
	defer ctrl.Finish()

	// Not IAAS model.
	canImplicitUpload := checkCanImplicitUpload(
		model.CAAS, true,
		semversion.MustParse("3.0.0"),
		semversion.MustParse("3.9.99.1"),
	)
	c.Check(canImplicitUpload, jc.IsFalse)

	// not official client.
	canImplicitUpload = checkCanImplicitUpload(
		model.IAAS, false,
		semversion.MustParse("3.9.99"),
		semversion.MustParse("3.0.0"),
	)
	c.Check(canImplicitUpload, jc.IsFalse)

	// non newer client.
	canImplicitUpload = checkCanImplicitUpload(
		model.IAAS, true,
		semversion.MustParse("2.9.99"),
		semversion.MustParse("3.0.0"),
	)
	c.Check(canImplicitUpload, jc.IsFalse)

	// client version with build number.
	canImplicitUpload = checkCanImplicitUpload(
		model.IAAS, true,
		semversion.MustParse("3.0.0.1"),
		semversion.MustParse("3.0.0"),
	)
	c.Check(canImplicitUpload, jc.IsTrue)

	// agent version with build number.
	canImplicitUpload = checkCanImplicitUpload(
		model.IAAS, true,
		semversion.MustParse("3.0.0"),
		semversion.MustParse("3.0.0.1"),
	)
	c.Check(canImplicitUpload, jc.IsTrue)

	// both client and agent version with build number == 0.
	canImplicitUpload = checkCanImplicitUpload(
		model.IAAS, true,
		semversion.MustParse("3.0.0"),
		semversion.MustParse("3.0.0"),
	)
	c.Check(canImplicitUpload, jc.IsFalse)
}
