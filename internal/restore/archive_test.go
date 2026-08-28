// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"testing"

	"github.com/juju/tc"

	corebackups "github.com/juju/juju/core/backups"
	domainexport "github.com/juju/juju/domain/export"
	"github.com/juju/juju/domain/export/types/controller/v4_1_0"
	"github.com/juju/juju/domain/export/types/latest"
	modelv4 "github.com/juju/juju/domain/export/types/v4_1_0"
)

type validateSuite struct{}

func TestValidateSuite(t *testing.T) {
	tc.Run(t, &validateSuite{})
}

func validMetadata() *corebackups.Metadata {
	m := corebackups.NewMetadata()
	m.Origin.Version = domainexport.LatestControllerExportVersion()
	m.Controller = corebackups.ControllerMetadata{HANodes: 1}
	return m
}

func validControllerExport() *v4_1_0.ControllerExport {
	return &v4_1_0.ControllerExport{
		Controller: []v4_1_0.Controller{{UUID: "ctrl", ModelUUID: "ctrl-model"}},
		Cloud:      []v4_1_0.Cloud{{UUID: "cloud", Name: "lxd", CloudTypeID: 1}},
		Model: []v4_1_0.Model{{
			UUID:      "ctrl-model",
			CloudUUID: "cloud",
		}},
	}
}

func (s *validateSuite) TestValidSourceAccepted(c *tc.C) {
	err := validateSource(validMetadata(), validControllerExport(), map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Check(err, tc.ErrorIsNil)
}

func (s *validateSuite) TestHARejected(c *tc.C) {
	m := validMetadata()
	m.Controller.HANodes = 3
	err := validateSource(m, validControllerExport(), map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Assert(err, tc.ErrorIs, ErrSourceUnsupported)
}

func (s *validateSuite) TestMultipleControllerRowsRejected(c *tc.C) {
	e := validControllerExport()
	e.Controller = append(e.Controller, v4_1_0.Controller{UUID: "other"})
	err := validateSource(validMetadata(), e, map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Assert(err, tc.ErrorIs, ErrSourceUnsupported)
}

func (s *validateSuite) TestModelDumpWithoutRowRejected(c *tc.C) {
	err := validateSource(validMetadata(), validControllerExport(), map[string]*latest.ModelExport{"other-model": {}})
	c.Assert(err, tc.ErrorIs, ErrArchiveInvalid)
}

func (s *validateSuite) TestModelDumpCountMismatchRejected(c *tc.C) {
	err := validateSource(validMetadata(), validControllerExport(), map[string]*latest.ModelExport{})
	c.Assert(err, tc.ErrorIs, ErrArchiveInvalid)
}

func (s *validateSuite) TestMultipleCloudsRejected(c *tc.C) {
	e := validControllerExport()
	e.Cloud = append(e.Cloud, v4_1_0.Cloud{UUID: "cloud2", Name: "lxd2", CloudTypeID: 1})
	err := validateSource(validMetadata(), e, map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Assert(err, tc.ErrorIs, ErrSourceUnsupported)
}

func (s *validateSuite) TestS3BackendRejected(c *tc.C) {
	e := validControllerExport()
	e.ObjectStoreBackend = []v4_1_0.ObjectStoreBackend{{UUID: "s3", TypeID: 1}}
	err := validateSource(validMetadata(), e, map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Assert(err, tc.ErrorIs, ErrSourceUnsupported)
}

func (s *validateSuite) TestS3ConfigRejected(c *tc.C) {
	e := validControllerExport()
	e.ObjectStoreBackendS3Config = []v4_1_0.ObjectStoreBackendS3Config{{ObjectStoreBackendUUID: "s3", Endpoint: "https://s3.example.com"}}
	err := validateSource(validMetadata(), e, map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Assert(err, tc.ErrorIs, ErrSourceUnsupported)
}

func (s *validateSuite) TestActiveDrainRejected(c *tc.C) {
	e := validControllerExport()
	e.ObjectStoreDrainInfo = []v4_1_0.ObjectStoreDrainInfo{{UUID: "drain", PhaseTypeID: 1, ToBackendUUID: "b"}}
	err := validateSource(validMetadata(), e, map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Assert(err, tc.ErrorIs, ErrSourceUnsupported)
}

func (s *validateSuite) TestCompletedDrainAccepted(c *tc.C) {
	e := validControllerExport()
	e.ObjectStoreDrainInfo = []v4_1_0.ObjectStoreDrainInfo{{UUID: "drain", PhaseTypeID: 3, ToBackendUUID: "b"}}
	err := validateSource(validMetadata(), e, map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Check(err, tc.ErrorIsNil)
}

func (s *validateSuite) TestUpgradeInProgressRejected(c *tc.C) {
	e := validControllerExport()
	e.UpgradeInfo = []v4_1_0.UpgradeInfo{{UUID: "up", PreviousVersion: "4.1.0", TargetVersion: "4.2.0"}}
	err := validateSource(validMetadata(), e, map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Assert(err, tc.ErrorIs, ErrSourceUnsupported)
}

func (s *validateSuite) TestMigratingModelRejected(c *tc.C) {
	models := map[string]*latest.ModelExport{"ctrl-model": {ModelMigrating: []modelv4.ModelMigrating{{ModelUUID: "ctrl-model"}}}}
	err := validateSource(validMetadata(), validControllerExport(), models)
	c.Assert(err, tc.ErrorIs, ErrSourceUnsupported)
}

func (s *validateSuite) TestUserSecretBackendRejected(c *tc.C) {
	e := validControllerExport()
	e.SecretBackend = []v4_1_0.SecretBackend{{UUID: "b", Name: "vault", OriginID: 1}}
	err := validateSource(validMetadata(), e, map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Assert(err, tc.ErrorIs, ErrSourceUnsupported)
}

func (s *validateSuite) TestBuiltinSecretBackendAccepted(c *tc.C) {
	e := validControllerExport()
	e.SecretBackend = []v4_1_0.SecretBackend{{UUID: "b", Name: "internal", OriginID: 0}}
	err := validateSource(validMetadata(), e, map[string]*latest.ModelExport{"ctrl-model": {}})
	c.Check(err, tc.ErrorIsNil)
}
