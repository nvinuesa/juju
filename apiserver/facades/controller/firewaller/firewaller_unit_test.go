// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package firewaller_test

import (
	"context"

	"github.com/juju/collections/set"
	"github.com/juju/names/v5"
	jc "github.com/juju/testing/checkers"
	"go.uber.org/mock/gomock"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/common"
	facademocks "github.com/juju/juju/apiserver/facade/mocks"
	"github.com/juju/juju/apiserver/facades/controller/firewaller"
	apiservertesting "github.com/juju/juju/apiserver/testing"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/status"
	"github.com/juju/juju/core/watcher/watchertest"
	"github.com/juju/juju/environs/config"
	loggertesting "github.com/juju/juju/internal/logger/testing"
	coretesting "github.com/juju/juju/internal/testing"
	jujutesting "github.com/juju/juju/juju/testing"
	"github.com/juju/juju/rpc/params"
	"github.com/juju/juju/state"
)

var _ = gc.Suite(&RemoteFirewallerSuite{})

type RemoteFirewallerSuite struct {
	coretesting.BaseSuite

	resources  *common.Resources
	authorizer *apiservertesting.FakeAuthorizer
	st         *MockState

	watcherRegistry     *facademocks.MockWatcherRegistry
	controllerConfigAPI *MockControllerConfigAPI
	api                 *firewaller.FirewallerAPI

	controllerConfigService *MockControllerConfigService
	modelConfigService      *MockModelConfigService
	networkService          *MockNetworkService
	applicationService      *MockApplicationService
}

func (s *RemoteFirewallerSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)

	s.resources = common.NewResources()
	s.AddCleanup(func(_ *gc.C) { s.resources.StopAll() })

	s.authorizer = &apiservertesting.FakeAuthorizer{
		Tag:        names.NewMachineTag("0"),
		Controller: true,
	}
}

func (s *RemoteFirewallerSuite) setupMocks(c *gc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)

	s.st = NewMockState(ctrl)

	s.watcherRegistry = facademocks.NewMockWatcherRegistry(ctrl)
	s.controllerConfigAPI = NewMockControllerConfigAPI(ctrl)

	s.controllerConfigService = NewMockControllerConfigService(ctrl)
	s.modelConfigService = NewMockModelConfigService(ctrl)
	s.networkService = NewMockNetworkService(ctrl)
	s.applicationService = NewMockApplicationService(ctrl)

	return ctrl
}

func (s *RemoteFirewallerSuite) setupAPI(c *gc.C) {
	var err error
	s.api, err = firewaller.NewStateFirewallerAPI(
		s.st,
		s.networkService,
		s.resources,
		s.watcherRegistry,
		s.authorizer,
		&mockCloudSpecAPI{},
		s.controllerConfigAPI,
		s.controllerConfigService,
		s.modelConfigService,
		s.applicationService,
		nil,
		loggertesting.WrapCheckLog(c),
	)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *RemoteFirewallerSuite) TestWatchIngressAddressesForRelations(c *gc.C) {
	ctrl := s.setupMocks(c)
	defer ctrl.Finish()
	s.setupAPI(c)

	db2Relation := newMockRelation(123)
	s.st.EXPECT().ModelUUID().Return(coretesting.ModelTag.Id()).AnyTimes()
	s.st.EXPECT().KeyRelation("remote-db2:db django:db").Return(db2Relation, nil)

	result, err := s.api.WatchIngressAddressesForRelations(
		context.Background(),
		params.Entities{Entities: []params.Entity{{
			Tag: names.NewRelationTag("remote-db2:db django:db").String(),
		}}},
	)

	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result.Results, gc.HasLen, 1)
	c.Assert(result.Results[0].Changes, jc.SameContents, []string{"1.2.3.4/32"})
	c.Assert(result.Results[0].Error, gc.IsNil)
	c.Assert(result.Results[0].StringsWatcherId, gc.Equals, "1")

	resource := s.resources.Get("1")
	c.Assert(resource, gc.NotNil)
	c.Assert(resource, gc.Implements, new(state.StringsWatcher))
}

func (s *RemoteFirewallerSuite) TestMacaroonForRelations(c *gc.C) {
	ctrl := s.setupMocks(c)
	defer ctrl.Finish()
	s.setupAPI(c)

	mac, err := jujutesting.NewMacaroon("apimac")
	c.Assert(err, jc.ErrorIsNil)
	entity := names.NewRelationTag("mysql:db wordpress:db")
	s.st.EXPECT().GetMacaroon(entity).Return(mac, nil)

	result, err := s.api.MacaroonForRelations(
		context.Background(),
		params.Entities{Entities: []params.Entity{{
			Tag: entity.String(),
		}}},
	)

	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result.Results, gc.HasLen, 1)
	c.Assert(result.Results[0].Error, gc.IsNil)
	c.Assert(result.Results[0].Result, jc.DeepEquals, mac)
}

func (s *RemoteFirewallerSuite) TestSetRelationStatus(c *gc.C) {
	ctrl := s.setupMocks(c)
	defer ctrl.Finish()
	s.setupAPI(c)

	db2Relation := newMockRelation(123)
	entity := names.NewRelationTag("remote-db2:db django:db")
	s.st.EXPECT().ModelUUID().Return(coretesting.ModelTag.Id()).AnyTimes()
	s.st.EXPECT().KeyRelation("remote-db2:db django:db").Return(db2Relation, nil)

	result, err := s.api.SetRelationsStatus(
		context.Background(),
		params.SetStatus{Entities: []params.EntityStatusArgs{{
			Tag:    entity.String(),
			Status: "suspended",
			Info:   "a message",
		}}})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result.Results, gc.HasLen, 1)
	c.Assert(result.Results[0].Error, gc.IsNil)
	c.Assert(db2Relation.status, jc.DeepEquals, status.StatusInfo{Status: status.Suspended, Message: "a message"})
}

var _ = gc.Suite(&FirewallerSuite{})

type FirewallerSuite struct {
	coretesting.BaseSuite

	authorizer *apiservertesting.FakeAuthorizer

	st *MockState

	watcherRegistry     *facademocks.MockWatcherRegistry
	controllerConfigAPI *MockControllerConfigAPI
	api                 *firewaller.FirewallerAPI

	controllerConfigService *MockControllerConfigService
	modelConfigService      *MockModelConfigService
	networkService          *MockNetworkService
	applicationService      *MockApplicationService
}

func (s *FirewallerSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)

	s.authorizer = &apiservertesting.FakeAuthorizer{
		Tag:        names.NewMachineTag("0"),
		Controller: true,
	}
}

func (s *FirewallerSuite) setupMocks(c *gc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)

	s.st = NewMockState(ctrl)

	s.watcherRegistry = facademocks.NewMockWatcherRegistry(ctrl)
	s.controllerConfigAPI = NewMockControllerConfigAPI(ctrl)

	s.controllerConfigService = NewMockControllerConfigService(ctrl)
	s.modelConfigService = NewMockModelConfigService(ctrl)
	s.networkService = NewMockNetworkService(ctrl)
	s.applicationService = NewMockApplicationService(ctrl)

	return ctrl
}

func (s *FirewallerSuite) setupAPI(c *gc.C) {
	var err error
	s.api, err = firewaller.NewStateFirewallerAPI(
		s.st,
		s.networkService,
		nil,
		s.watcherRegistry,
		s.authorizer,
		&mockCloudSpecAPI{},
		s.controllerConfigAPI,
		s.controllerConfigService,
		s.modelConfigService,
		s.applicationService,
		nil,
		loggertesting.WrapCheckLog(c),
	)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *FirewallerSuite) TestModelFirewallRules(c *gc.C) {
	ctrl := s.setupMocks(c)
	defer ctrl.Finish()
	s.setupAPI(c)

	s.controllerConfigService.EXPECT().ControllerConfig(gomock.Any()).Return(controller.NewConfig(coretesting.ControllerTag.Id(), coretesting.CACert, map[string]interface{}{}))

	modelAttrs := coretesting.FakeConfig().Merge(map[string]interface{}{
		config.SSHAllowKey: "192.168.0.0/24,192.168.1.0/24",
	})
	s.modelConfigService.EXPECT().ModelConfig(gomock.Any()).Return(config.New(config.UseDefaults, modelAttrs))
	s.st.EXPECT().IsController().Return(false)

	rules, err := s.api.ModelFirewallRules(context.Background())

	c.Assert(err, jc.ErrorIsNil)
	c.Assert(rules, gc.DeepEquals, params.IngressRulesResult{Rules: []params.IngressRule{{
		PortRange:   params.FromNetworkPortRange(network.MustParsePortRange("22")),
		SourceCIDRs: []string{"192.168.0.0/24", "192.168.1.0/24"},
	}}})
}

func (s *FirewallerSuite) TestModelFirewallRulesController(c *gc.C) {
	ctrl := s.setupMocks(c)
	defer ctrl.Finish()
	s.setupAPI(c)

	ctrlAttrs := map[string]interface{}{
		controller.APIPort:            17777,
		controller.AutocertDNSNameKey: "example.com",
	}
	s.controllerConfigService.EXPECT().ControllerConfig(gomock.Any()).Return(controller.NewConfig(coretesting.ControllerTag.Id(), coretesting.CACert, ctrlAttrs))

	modelAttrs := coretesting.FakeConfig().Merge(map[string]interface{}{
		config.SSHAllowKey: "192.168.0.0/24,192.168.1.0/24",
	})
	s.modelConfigService.EXPECT().ModelConfig(gomock.Any()).Return(config.New(config.UseDefaults, modelAttrs))
	s.st.EXPECT().IsController().Return(true)

	rules, err := s.api.ModelFirewallRules(context.Background())

	c.Assert(err, jc.ErrorIsNil)
	c.Assert(rules, gc.DeepEquals, params.IngressRulesResult{Rules: []params.IngressRule{{
		PortRange:   params.FromNetworkPortRange(network.MustParsePortRange("22")),
		SourceCIDRs: []string{"192.168.0.0/24", "192.168.1.0/24"},
	}, {
		PortRange:   params.FromNetworkPortRange(network.MustParsePortRange("17777")),
		SourceCIDRs: []string{"0.0.0.0/0", "::/0"},
	}, {
		PortRange:   params.FromNetworkPortRange(network.MustParsePortRange("80")),
		SourceCIDRs: []string{"0.0.0.0/0", "::/0"},
	}}})
}

func (s *FirewallerSuite) TestWatchModelFirewallRules(c *gc.C) {
	ctrl := s.setupMocks(c)
	defer ctrl.Finish()
	s.setupAPI(c)

	ch := make(chan []string, 1)
	// initial event
	ch <- []string{}
	w := watchertest.NewMockStringsWatcher(ch)

	s.modelConfigService.EXPECT().Watch().Return(w, nil)
	s.modelConfigService.EXPECT().ModelConfig(gomock.Any()).Return(config.New(config.UseDefaults, coretesting.FakeConfig()))

	s.watcherRegistry.EXPECT().Register(gomock.Any()).Return("1", nil)

	result, err := s.api.WatchModelFirewallRules(context.Background())
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result.Error, gc.IsNil)
	c.Assert(result.NotifyWatcherId, gc.Equals, "1")
}

func (s *FirewallerSuite) TestOpenedMachinePortRanges(c *gc.C) {
	ctrl := s.setupMocks(c)
	defer ctrl.Finish()
	s.setupAPI(c)

	// Set up our mocks
	mockMachine := newMockMachine("0")
	mockMachine.openedPortRanges = newMockMachinePortRanges(
		newMockUnitPortRanges(
			"wordpress/0",
			network.GroupedPortRanges{
				"": []network.PortRange{
					network.MustParsePortRange("80/tcp"),
				},
			},
		),
		newMockUnitPortRanges(
			"mysql/0",
			network.GroupedPortRanges{
				"foo": []network.PortRange{
					network.MustParsePortRange("3306/tcp"),
				},
			},
		),
	)
	spaceInfos := network.SpaceInfos{
		{ID: network.AlphaSpaceId, Name: "alpha", Subnets: []network.SubnetInfo{
			{ID: "11", CIDR: "10.0.0.0/24"},
			{ID: "12", CIDR: "10.0.1.0/24"},
		}},
		{ID: "42", Name: "questions-about-the-universe", Subnets: []network.SubnetInfo{
			{ID: "13", CIDR: "192.168.0.0/24"},
			{ID: "14", CIDR: "192.168.1.0/24"},
		}},
	}
	applicationEndpointBindings := map[string]map[string]string{
		"mysql": {
			"":    network.AlphaSpaceId,
			"foo": "42",
		},
		"wordpress": {
			"":           network.AlphaSpaceId,
			"monitoring": network.AlphaSpaceId,
			"web":        "42",
		},
	}
	s.st.EXPECT().Machine("0").Return(mockMachine, nil)
	s.networkService.EXPECT().GetAllSpaces(gomock.Any()).Return(spaceInfos, nil)
	s.st.EXPECT().AllEndpointBindings().Return(applicationEndpointBindings, nil)

	// Test call output
	req := params.Entities{
		Entities: []params.Entity{
			{Tag: names.NewMachineTag("0").String()},
		},
	}
	res, err := s.api.OpenedMachinePortRanges(context.Background(), req)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(res.Results, gc.HasLen, 1)

	c.Assert(res.Results[0].Error, gc.IsNil)
	c.Assert(res.Results[0].UnitPortRanges, gc.DeepEquals, map[string][]params.OpenUnitPortRanges{
		"unit-wordpress-0": {
			{
				Endpoint:    "",
				SubnetCIDRs: []string{"10.0.0.0/24", "10.0.1.0/24", "192.168.0.0/24", "192.168.1.0/24"},
				PortRanges: []params.PortRange{
					params.FromNetworkPortRange(network.MustParsePortRange("80/tcp")),
				},
			},
		},
		"unit-mysql-0": {
			{
				Endpoint:    "foo",
				SubnetCIDRs: []string{"192.168.0.0/24", "192.168.1.0/24"},
				PortRanges: []params.PortRange{
					params.FromNetworkPortRange(network.MustParsePortRange("3306/tcp")),
				},
			},
		},
	})
}

func (s *FirewallerSuite) TestAllSpaceInfos(c *gc.C) {
	ctrl := s.setupMocks(c)
	defer ctrl.Finish()
	s.setupAPI(c)

	// Set up our mocks
	spaceInfos := network.SpaceInfos{
		{
			ID:         "42",
			Name:       "questions-about-the-universe",
			ProviderId: "provider-id-2",
			Subnets: []network.SubnetInfo{
				{
					ID:                "13",
					CIDR:              "1.168.1.0/24",
					ProviderId:        "provider-subnet-id-1",
					ProviderSpaceId:   "provider-space-id-1",
					ProviderNetworkId: "provider-network-id-1",
					VLANTag:           42,
					AvailabilityZones: []string{"az1", "az2"},
					SpaceID:           "42",
					SpaceName:         "questions-about-the-universe",
				},
			}},
		{ID: "99", Name: "special", Subnets: []network.SubnetInfo{
			{ID: "999", CIDR: "192.168.2.0/24"},
		}},
	}
	s.networkService.EXPECT().GetAllSpaces(gomock.Any()).Return(spaceInfos, nil)

	// Test call output
	req := params.SpaceInfosParams{
		FilterBySpaceIDs: []string{network.AlphaSpaceId, "42"},
	}
	res, err := s.api.SpaceInfos(context.Background(), req)
	c.Assert(err, jc.ErrorIsNil)

	// Hydrate a network.SpaceInfos from the response
	gotSpaceInfos := params.ToNetworkSpaceInfos(res)
	c.Assert(gotSpaceInfos, gc.DeepEquals, spaceInfos[0:1], gc.Commentf("expected to get back a filtered list of the space infos"))
}

func (s *FirewallerSuite) TestWatchSubnets(c *gc.C) {
	ctrl := s.setupMocks(c)
	defer ctrl.Finish()
	s.setupAPI(c)

	ch := make(chan []string, 1)
	ch <- []string{"100"}
	w := watchertest.NewMockStringsWatcher(ch)
	s.networkService.EXPECT().WatchSubnets(gomock.Any(), set.NewStrings("100")).Return(w, nil)

	s.watcherRegistry.EXPECT().Register(gomock.Any()).Return("1", nil)

	entities := params.Entities{
		Entities: []params.Entity{{
			Tag: names.NewSubnetTag("100").String(),
		}},
	}
	got, err := s.api.WatchSubnets(context.Background(), entities)
	c.Assert(err, jc.ErrorIsNil)
	want := params.StringsWatchResult{
		StringsWatcherId: "1",
		Changes:          []string{"100"},
	}
	c.Assert(got.StringsWatcherId, gc.Equals, want.StringsWatcherId)
	c.Assert(got.Changes, jc.SameContents, want.Changes)
}
