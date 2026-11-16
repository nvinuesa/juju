// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"context"
	stdtesting "testing"

	"github.com/juju/tc"

	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/internal/errors"
	loggertesting "github.com/juju/juju/internal/logger/testing"
	"github.com/juju/juju/internal/recovery"
	"github.com/juju/juju/internal/testhelpers"
)

type stageSuite struct {
	testhelpers.IsolationSuite
}

func TestStageSuite(t *stdtesting.T) {
	tc.Run(t, &stageSuite{})
}

type recordingRecoveryService struct {
	controllerDump []byte
	modelDumps     map[string][]byte
	order          []string
	controllerErr  error
	modelErr       error
}

func (s *recordingRecoveryService) LoadControllerDump(_ context.Context, data []byte) error {
	s.order = append(s.order, "controller")
	s.controllerDump = append([]byte(nil), data...)
	return s.controllerErr
}

func (s *recordingRecoveryService) LoadModelDump(_ context.Context, model coremodel.UUID, data []byte) error {
	s.order = append(s.order, model.String())
	if s.modelDumps == nil {
		s.modelDumps = make(map[string][]byte)
	}
	s.modelDumps[model.String()] = append([]byte(nil), data...)
	return s.modelErr
}

func (s *stageSuite) TestLoadReplaysControllerAndSortedModels(c *tc.C) {
	path, sum := writeArchive(c, validFiles())
	svc := &recordingRecoveryService{}

	err := recovery.Load(c.Context(), recovery.LoadParams{
		Service:             svc,
		ArchivePath:         path,
		ExpectedSHA256:      sum,
		DataDir:             c.MkDir(),
		ControllerModelUUID: testControllerModelUUID,
		Logger:              loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(svc.controllerDump, tc.Not(tc.HasLen), 0)
	c.Check(svc.order, tc.DeepEquals, []string{
		"controller", testControllerModelUUID, testModelAUUID, testModelBUUID,
	})
	c.Check(svc.modelDumps[testControllerModelUUID], tc.Not(tc.HasLen), 0)
	c.Check(svc.modelDumps[testModelAUUID], tc.Not(tc.HasLen), 0)
	c.Check(svc.modelDumps[testModelBUUID], tc.Not(tc.HasLen), 0)
}

func (s *stageSuite) TestLoadRejectsAnotherControllerModel(c *tc.C) {
	path, sum := writeArchive(c, validFiles())
	svc := &recordingRecoveryService{}

	err := recovery.Load(c.Context(), recovery.LoadParams{
		Service:             svc,
		ArchivePath:         path,
		ExpectedSHA256:      sum,
		DataDir:             c.MkDir(),
		ControllerModelUUID: testModelAUUID,
		Logger:              loggertesting.WrapCheckLog(c),
	})
	c.Check(err, tc.ErrorMatches,
		`archive controller model "`+testControllerModelUUID+
			`" does not match the bootstrap controller model "`+testModelAUUID+`"`)
	c.Check(svc.order, tc.HasLen, 0)
}

func (s *stageSuite) TestLoadRejectsChecksumMismatch(c *tc.C) {
	path, _ := writeArchive(c, validFiles())
	svc := &recordingRecoveryService{}

	err := recovery.Load(c.Context(), recovery.LoadParams{
		Service:             svc,
		ArchivePath:         path,
		ExpectedSHA256:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		DataDir:             c.MkDir(),
		ControllerModelUUID: testControllerModelUUID,
		Logger:              loggertesting.WrapCheckLog(c),
	})
	c.Check(err, tc.ErrorMatches, "archive checksum mismatch.*")
	c.Check(svc.order, tc.HasLen, 0)
}

func (s *stageSuite) TestLoadPropagatesControllerFailure(c *tc.C) {
	path, sum := writeArchive(c, validFiles())
	svc := &recordingRecoveryService{controllerErr: errors.New("controller failed")}

	err := recovery.Load(c.Context(), recovery.LoadParams{
		Service:             svc,
		ArchivePath:         path,
		ExpectedSHA256:      sum,
		DataDir:             c.MkDir(),
		ControllerModelUUID: testControllerModelUUID,
		Logger:              loggertesting.WrapCheckLog(c),
	})
	c.Check(err, tc.ErrorMatches, "controller failed")
	c.Check(svc.order, tc.DeepEquals, []string{"controller"})
}
