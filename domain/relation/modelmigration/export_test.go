// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelmigration

import (
	"context"

	"github.com/juju/description/v9"
	jc "github.com/juju/testing/checkers"
	"go.uber.org/mock/gomock"
	gc "gopkg.in/check.v1"

	corerelationtesting "github.com/juju/juju/core/relation/testing"
	"github.com/juju/juju/domain/relation"
	"github.com/juju/juju/internal/charm"
	"github.com/juju/juju/internal/errors"
	loggertesting "github.com/juju/juju/internal/logger/testing"
)

type exportSuite struct {
	coordinator   *MockCoordinator
	exportService *MockExportService
}

var _ = gc.Suite(&exportSuite{})

func (s *exportSuite) setupMocks(c *gc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)

	s.coordinator = NewMockCoordinator(ctrl)
	s.exportService = NewMockExportService(ctrl)

	return ctrl
}

func (s *exportSuite) newExportOperation(c *gc.C) *exportOperation {
	return &exportOperation{
		exportService: s.exportService,
		logger:        loggertesting.WrapCheckLog(c),
	}
}
func (s *exportSuite) TestExport(c *gc.C) {
	defer s.setupMocks(c).Finish()

	// Arrange:
	model := description.NewModel(description.ModelArgs{})
	data := []relation.ExportRelation{{
		ID:  7,
		Key: corerelationtesting.GenNewKey(c, "app1:key, app2:key"),
		Endpoints: []relation.ExportEndpoint{{
			ApplicationName: "fake-app-1",
			Name:            "fake-endpoint-name-1",
			Role:            charm.RoleProvider,
			Interface:       "database",
			Optional:        true,
			Limit:           20,
			Scope:           charm.ScopeGlobal,
			AllUnitSettings: map[string]map[string]any{
				"app1/0": {
					"unit1-foo": "unit1-bar",
				},
				"app1/1": {
					"unit2-foo": "unit2-bar",
				},
			},
			ApplicationSettings: make(map[string]any),
		}, {
			ApplicationName: "fake-app-2",
			Name:            "fake-endpoint-name-2",
			Role:            charm.RoleRequirer,
			Interface:       "database",
			Optional:        false,
			Limit:           10,
			Scope:           charm.ScopeGlobal,
			ApplicationSettings: map[string]any{
				"app-foo": "app-bar",
			},
			AllUnitSettings: make(map[string]map[string]any),
		}},
	}, {
		ID:  8,
		Key: corerelationtesting.GenNewKey(c, "app1:key"),
		Endpoints: []relation.ExportEndpoint{{
			ApplicationName:     "fake-app-1",
			Name:                "fake-endpoint-name-1",
			Role:                charm.RolePeer,
			Interface:           "database",
			Optional:            true,
			Limit:               20,
			Scope:               charm.ScopeContainer,
			ApplicationSettings: make(map[string]any),
			AllUnitSettings:     make(map[string]map[string]any),
		}},
	}}
	s.exportService.EXPECT().ExportRelations(gomock.Any()).Return(data, nil)

	// Act:
	op := s.newExportOperation(c)
	err := op.Execute(context.Background(), model)
	c.Assert(err, jc.ErrorIsNil)

	// Assert:
	relations := model.Relations()
	c.Assert(relations, gc.HasLen, 2)

	c.Check(relations[0].Id(), gc.Equals, data[0].ID)
	c.Check(relations[0].Key(), gc.Equals, data[0].Key.String())
	c.Check(relations[0].Suspended(), gc.Equals, false)
	endpoints := relations[0].Endpoints()
	c.Assert(endpoints, gc.HasLen, 2)
	s.assertEndpointsMatch(c, endpoints[0], data[0].Endpoints[0])
	s.assertEndpointsMatch(c, endpoints[1], data[0].Endpoints[1])

	c.Check(relations[1].Id(), gc.Equals, data[1].ID)
	c.Check(relations[1].Key(), gc.Equals, data[1].Key.String())
	c.Check(relations[1].Suspended(), gc.Equals, false)
	endpoints = relations[1].Endpoints()
	c.Assert(endpoints, gc.HasLen, 1)
	s.assertEndpointsMatch(c, endpoints[0], data[1].Endpoints[0])
}

func (s *exportSuite) TestExportEmpty(c *gc.C) {
	defer s.setupMocks(c).Finish()

	// Arrange:
	model := description.NewModel(description.ModelArgs{})
	s.exportService.EXPECT().ExportRelations(gomock.Any()).Return(nil, nil)

	// Act:
	op := s.newExportOperation(c)
	err := op.Execute(context.Background(), model)
	c.Assert(err, jc.ErrorIsNil)

	// Assert:
	relations := model.Relations()
	c.Assert(relations, gc.HasLen, 0)
}

func (s *exportSuite) TestExportServiceError(c *gc.C) {
	defer s.setupMocks(c).Finish()

	// Arrange:
	model := description.NewModel(description.ModelArgs{})
	boom := errors.New("boom")
	s.exportService.EXPECT().ExportRelations(gomock.Any()).Return(nil, boom)

	// Act:
	op := s.newExportOperation(c)
	err := op.Execute(context.Background(), model)

	// Assert:
	c.Assert(err, jc.ErrorIs, boom)
}

func (s *exportSuite) assertEndpointsMatch(c *gc.C, ep description.Endpoint, expected relation.ExportEndpoint) {
	c.Check(ep.ApplicationName(), gc.Equals, expected.ApplicationName)
	c.Check(ep.Name(), gc.Equals, expected.Name)
	c.Check(ep.Role(), gc.Equals, string(expected.Role))
	c.Check(ep.Interface(), gc.Equals, expected.Interface)
	c.Check(ep.Optional(), gc.Equals, expected.Optional)
	c.Check(ep.Limit(), gc.Equals, expected.Limit)
	c.Check(ep.Scope(), gc.Equals, string(expected.Scope))
	c.Check(ep.ApplicationSettings(), gc.DeepEquals, expected.ApplicationSettings)
	c.Check(ep.AllSettings(), gc.DeepEquals, expected.AllUnitSettings)
}
