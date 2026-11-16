// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"context"
	"os"
	"path/filepath"
	stdtesting "testing"

	"github.com/juju/tc"

	coremodel "github.com/juju/juju/core/model"
	domainrecovery "github.com/juju/juju/domain/recovery"
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
	controllerDump  []byte
	modelDumps      map[string][]byte
	order           []string
	controllerErr   error
	modelErr        error
	machinePatch    *domainrecovery.MachinePatch
	machineName     string
	unitMachine     string
	unitAddresses   []string
	credential      *recovery.CredentialPatch
	inventoryModels []coremodel.UUID
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

func (s *recordingRecoveryService) PatchControllerMachine(_ context.Context, _ coremodel.UUID, patch domainrecovery.MachinePatch) (string, error) {
	s.order = append(s.order, "machine")
	s.machinePatch = &patch
	if s.machineName == "" {
		s.machineName = "0"
	}
	return s.machineName, nil
}

func (s *recordingRecoveryService) PatchControllerUnitAddresses(_ context.Context, _ coremodel.UUID, machineName string, values []string) error {
	s.order = append(s.order, "addresses")
	s.unitMachine = machineName
	s.unitAddresses = values
	return nil
}

func (s *recordingRecoveryService) PatchControllerCredential(_ context.Context, _ string, authType string, attributes map[string]string) error {
	s.order = append(s.order, "credential")
	s.credential = &recovery.CredentialPatch{AuthType: authType, Attributes: attributes}
	return nil
}

func (s *recordingRecoveryService) GetObjectInventory(_ context.Context, models []coremodel.UUID) ([]domainrecovery.ObjectInventory, error) {
	s.order = append(s.order, "inventory")
	s.inventoryModels = models
	return []domainrecovery.ObjectInventory{{Namespace: "controller"}}, nil
}

func (s *stageSuite) TestLoadReplaysControllerAndSortedModels(c *tc.C) {
	path, sum := writeArchive(c, validFiles())
	svc := &recordingRecoveryService{}

	err := recovery.Load(c.Context(), recovery.LoadParams{
		Service:             svc,
		ArchivePath:         path,
		ExpectedSHA256:      sum,
		DataDir:             c.MkDir(),
		ControllerID:        "0",
		ControllerModelUUID: testControllerModelUUID,
		Logger:              loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(svc.controllerDump, tc.Not(tc.HasLen), 0)
	c.Check(svc.order, tc.DeepEquals, []string{
		"controller", testControllerModelUUID, testModelAUUID, testModelBUUID, "inventory",
	})
	c.Check(svc.inventoryModels, tc.DeepEquals, []coremodel.UUID{
		testControllerModelUUID, testModelAUUID, testModelBUUID,
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
		ControllerID:        "0",
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
		ControllerID:        "0",
		ControllerModelUUID: testControllerModelUUID,
		Logger:              loggertesting.WrapCheckLog(c),
	})
	c.Check(err, tc.ErrorMatches, "archive checksum mismatch.*")
	c.Check(svc.order, tc.HasLen, 0)
}

func (s *stageSuite) TestLoadAppliesReplacementFacts(c *tc.C) {
	path, sum := writeArchive(c, validFiles())
	svc := &recordingRecoveryService{}
	dataDir := c.MkDir()

	err := recovery.Load(c.Context(), recovery.LoadParams{
		Service:             svc,
		ControllerID:        "0",
		ControllerModelUUID: testControllerModelUUID,
		ArchivePath:         path,
		ExpectedSHA256:      sum,
		DataDir:             dataDir,
		MachinePatch: &domainrecovery.MachinePatch{
			InstanceID:  "replacement-instance",
			DisplayName: "replacement",
			Nonce:       "replacement-nonce",
		},
		ControllerUnitAddresses: []string{"10.0.0.5"},
		ControllerCredential: &recovery.CredentialPatch{
			AuthType:   "access-key",
			Attributes: map[string]string{"key": "value"},
		},
		Logger: loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(svc.order[len(svc.order)-3:], tc.DeepEquals,
		[]string{"machine", "addresses", "credential"})
	c.Assert(svc.machinePatch, tc.NotNil)
	c.Check(svc.machinePatch.InstanceID, tc.Equals, "replacement-instance")
	c.Check(svc.machinePatch.DisplayName, tc.Equals, "replacement")
	c.Check(svc.machinePatch.Nonce, tc.Equals, "replacement-nonce")
	c.Check(svc.unitMachine, tc.Equals, "0")
	c.Check(svc.unitAddresses, tc.DeepEquals, []string{"10.0.0.5"})
	c.Assert(svc.credential, tc.NotNil)
	c.Check(svc.credential.AuthType, tc.Equals, "access-key")
	c.Check(svc.credential.Attributes, tc.DeepEquals, map[string]string{"key": "value"})

	confPath := filepath.Join(dataDir, "agents", "controller-0", "controller.conf")
	info, err := os.Stat(confPath)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(info.Mode().Perm(), tc.Equals, os.FileMode(0600))
	c.Check(info.Size(), tc.Equals, int64(0))
}

func (s *stageSuite) TestLoadPropagatesControllerFailure(c *tc.C) {
	path, sum := writeArchive(c, validFiles())
	svc := &recordingRecoveryService{controllerErr: errors.New("controller failed")}

	err := recovery.Load(c.Context(), recovery.LoadParams{
		Service:             svc,
		ArchivePath:         path,
		ExpectedSHA256:      sum,
		DataDir:             c.MkDir(),
		ControllerID:        "0",
		ControllerModelUUID: testControllerModelUUID,
		Logger:              loggertesting.WrapCheckLog(c),
	})
	c.Check(err, tc.ErrorMatches, "controller failed")
	c.Check(svc.order, tc.DeepEquals, []string{"controller"})
}
