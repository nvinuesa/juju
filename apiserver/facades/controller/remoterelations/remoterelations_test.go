// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package remoterelations_test

import (
	"testing"

	"github.com/juju/errors"
	"github.com/juju/names/v6"
	"github.com/juju/tc"
	"go.uber.org/mock/gomock"

	"github.com/juju/juju/apiserver/facades/controller/remoterelations"
	"github.com/juju/juju/apiserver/facades/controller/remoterelations/mocks"
	apiservertesting "github.com/juju/juju/apiserver/testing"
	"github.com/juju/juju/core/crossmodel"
	"github.com/juju/juju/core/secrets"
	coretesting "github.com/juju/juju/internal/testing"
	"github.com/juju/juju/internal/uuid"
	"github.com/juju/juju/rpc/params"
)

func TestRemoteRelationsSuite(t *testing.T) {
	tc.Run(t, &remoteRelationsSuite{})
}

type remoteRelationsSuite struct {
	coretesting.BaseSuite

	authorizer            *apiservertesting.FakeAuthorizer
	ecService             *mocks.MockExternalControllerService
	secretService         *mocks.MockSecretService
	crossModelRelationSvc *mocks.MockCrossModelRelationService
	cc                    *mocks.MockControllerConfigAPI
	api                   *remoterelations.API
}

func (s *remoteRelationsSuite) SetUpTest(c *tc.C) {
	s.BaseSuite.SetUpTest(c)

	s.authorizer = &apiservertesting.FakeAuthorizer{
		Tag:        names.NewMachineTag("0"),
		Controller: true,
	}
}

func (s *remoteRelationsSuite) setup(c *tc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)

	s.cc = mocks.NewMockControllerConfigAPI(ctrl)
	s.ecService = mocks.NewMockExternalControllerService(ctrl)
	s.secretService = mocks.NewMockSecretService(ctrl)
	s.crossModelRelationSvc = mocks.NewMockCrossModelRelationService(ctrl)
	api, err := remoterelations.NewRemoteRelationsAPI(
		s.ecService,
		s.secretService,
		s.crossModelRelationSvc,
		s.watcherRegistry,
		s.cc,
		s.authorizer,
	)
	c.Assert(err, tc.ErrorIsNil)
	s.api = api
	return ctrl
}

func (s *remoteRelationsSuite) TestUpdateControllersForModels(c *tc.C) {
	defer s.setup(c).Finish()

	mod1 := uuid.MustNewUUID().String()
	c1Tag := names.NewControllerTag(uuid.MustNewUUID().String())
	mod2 := uuid.MustNewUUID().String()
	c2Tag := names.NewControllerTag(uuid.MustNewUUID().String())

	c1 := crossmodel.ControllerInfo{
		ControllerUUID: c1Tag.Id(),
		Alias:          "alias1",
		Addrs:          []string{"1.1.1.1:1"},
		CACert:         "cert1",
		ModelUUIDs:     []string{mod1},
	}
	c2 := crossmodel.ControllerInfo{
		ControllerUUID: c2Tag.Id(),
		Alias:          "alias2",
		Addrs:          []string{"2.2.2.2:2"},
		CACert:         "cert2",
		ModelUUIDs:     []string{mod2},
	}

	s.ecService.EXPECT().UpdateExternalController(
		gomock.Any(),
		c1,
	).Return(errors.New("whack"))
	s.ecService.EXPECT().UpdateExternalController(
		gomock.Any(),
		c2,
	).Return(nil)

	res, err := s.api.UpdateControllersForModels(
		c.Context(),
		params.UpdateControllersForModelsParams{
			Changes: []params.UpdateControllerForModel{
				{
					ModelTag: names.NewModelTag(mod1).String(),
					Info: params.ExternalControllerInfo{
						ControllerTag: c1Tag.String(),
						Alias:         "alias1",
						Addrs:         []string{"1.1.1.1:1"},
						CACert:        "cert1",
					},
				},
				{
					ModelTag: names.NewModelTag(mod2).String(),
					Info: params.ExternalControllerInfo{
						ControllerTag: c2Tag.String(),
						Alias:         "alias2",
						Addrs:         []string{"2.2.2.2:2"},
						CACert:        "cert2",
					},
				},
			},
		})

	c.Assert(err, tc.ErrorIsNil)
	c.Assert(res.Results, tc.HasLen, 2)
	c.Assert(res.Results[0].Error.Message, tc.Equals, "whack")
	c.Assert(res.Results[1].Error, tc.IsNil)
}

func (s *remoteRelationsSuite) TestConsumeRemoteSecretChanges(c *tc.C) {
	defer s.setup(c).Finish()

	uri := secrets.NewURI()
	change := params.SecretRevisionChange{
		URI:            uri.String(),
		LatestRevision: 666,
	}
	changes := params.LatestSecretRevisionChanges{
		Changes: []params.SecretRevisionChange{change},
	}

	s.secretService.EXPECT().UpdateRemoteSecretRevision(gomock.Any(), uri, 666).Return(nil)

	result, err := s.api.ConsumeRemoteSecretChanges(c.Context(), changes)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(result.OneError(), tc.IsNil)
}

func (s *remoteRelationsSuite) TestWatchRemoteApplicationRelations(c *tc.C) {
	defer s.setup(c).Finish()

	// This test just ensures that the method can be called without errors.
	// A full test would require more complex watcher setup.
	appTag := names.NewApplicationTag("mysql")
	args := params.Entities{
		Entities: []params.Entity{
			{Tag: appTag.String()},
		},
	}

	// The test should handle the case where the application doesn't exist
	s.crossModelRelationSvc.EXPECT().WatchRemoteApplicationRelations(gomock.Any(), "mysql").Return(nil, errors.New("application not found"))

	result, err := s.api.WatchRemoteApplicationRelations(c.Context(), args)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(result.Results, tc.HasLen, 1)
	c.Assert(result.Results[0].Error, tc.Not(tc.IsNil))
}

func (s *remoteRelationsSuite) TestWatchRemoteApplicationRelationsSuccess(c *tc.C) {
	defer s.setup(c).Finish()

	appTag := names.NewApplicationTag("mysql")
	args := params.Entities{
		Entities: []params.Entity{
			{Tag: appTag.String()},
		},
	}

	mockWatcher := &mockStringsWatcher{changes: make(chan []string)}

	s.crossModelRelationSvc.EXPECT().WatchRemoteApplicationRelations(gomock.Any(), "mysql").Return(mockWatcher, nil)
	s.watcherRegistry.EXPECT().Register(gomock.Any()).Return("watcher-1", nil)

	result, err := s.api.WatchRemoteApplicationRelations(c.Context(), args)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(result.Results, tc.HasLen, 1)
	c.Assert(result.Results[0].Error, tc.IsNil)
	c.Assert(result.Results[0].StringsWatcherId, tc.Equals, "watcher-1")
}

func (s *remoteRelationsSuite) TestWatchRemoteApplicationRelationsMultipleEntities(c *tc.C) {
	defer s.setup(c).Finish()

	args := params.Entities{
		Entities: []params.Entity{
			{Tag: names.NewApplicationTag("mysql").String()},
			{Tag: names.NewApplicationTag("postgres").String()},
		},
	}

	mockWatcher1 := &mockStringsWatcher{changes: make(chan []string)}
	mockWatcher2 := &mockStringsWatcher{changes: make(chan []string)}

	s.crossModelRelationSvc.EXPECT().WatchRemoteApplicationRelations(gomock.Any(), "mysql").Return(mockWatcher1, nil)
	s.watcherRegistry.EXPECT().Register(gomock.Any()).Return("watcher-1", nil)

	s.crossModelRelationSvc.EXPECT().WatchRemoteApplicationRelations(gomock.Any(), "postgres").Return(mockWatcher2, nil)
	s.watcherRegistry.EXPECT().Register(gomock.Any()).Return("watcher-2", nil)

	result, err := s.api.WatchRemoteApplicationRelations(c.Context(), args)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(result.Results, tc.HasLen, 2)
	c.Assert(result.Results[0].Error, tc.IsNil)
	c.Assert(result.Results[0].StringsWatcherId, tc.Equals, "watcher-1")
	c.Assert(result.Results[1].Error, tc.IsNil)
	c.Assert(result.Results[1].StringsWatcherId, tc.Equals, "watcher-2")
}

func (s *remoteRelationsSuite) TestWatchRemoteApplicationRelationsInvalidTag(c *tc.C) {
	defer s.setup(c).Finish()

	args := params.Entities{
		Entities: []params.Entity{
			{Tag: "invalid-tag"},
		},
	}

	result, err := s.api.WatchRemoteApplicationRelations(c.Context(), args)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(result.Results, tc.HasLen, 1)
	c.Assert(result.Results[0].Error, tc.Not(tc.IsNil))
	c.Assert(result.Results[0].Error.Message, tc.Matches, `.*invalid.*tag.*`)
}

func (s *remoteRelationsSuite) TestWatchRemoteApplicationRelationsWatcherRegistrationError(c *tc.C) {
	defer s.setup(c).Finish()

	appTag := names.NewApplicationTag("mysql")
	args := params.Entities{
		Entities: []params.Entity{
			{Tag: appTag.String()},
		},
	}

	mockWatcher := &mockStringsWatcher{changes: make(chan []string)}

	s.crossModelRelationSvc.EXPECT().WatchRemoteApplicationRelations(gomock.Any(), "mysql").Return(mockWatcher, nil)
	s.watcherRegistry.EXPECT().Register(gomock.Any()).Return("", errors.New("registration failed"))

	result, err := s.api.WatchRemoteApplicationRelations(c.Context(), args)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(result.Results, tc.HasLen, 1)
	c.Assert(result.Results[0].Error, tc.Not(tc.IsNil))
	c.Assert(result.Results[0].Error.Message, tc.Matches, `.*registration failed.*`)
}

// mockStringsWatcher is a simple mock implementation of watcher.StringsWatcher
type mockStringsWatcher struct {
	changes chan []string
}

func (m *mockStringsWatcher) Changes() <-chan []string {
	return m.changes
}

func (m *mockStringsWatcher) Kill() {
	close(m.changes)
}

func (m *mockStringsWatcher) Wait() error {
	return nil
}
