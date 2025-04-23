// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"
	"reflect"
	"time"

	jc "github.com/juju/testing/checkers"
	"github.com/kr/pretty"
	"go.uber.org/mock/gomock"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/caas"
	coreapplication "github.com/juju/juju/core/application"
	applicationtesting "github.com/juju/juju/core/application/testing"
	coreerrors "github.com/juju/juju/core/errors"
	"github.com/juju/juju/core/network"
	corestatus "github.com/juju/juju/core/status"
	coreunit "github.com/juju/juju/core/unit"
	unittesting "github.com/juju/juju/core/unit/testing"
	"github.com/juju/juju/domain/application"
	applicationerrors "github.com/juju/juju/domain/application/errors"
	"github.com/juju/juju/domain/life"
	"github.com/juju/juju/domain/status"
	"github.com/juju/juju/internal/errors"
)

type unitServiceSuite struct {
	baseSuite
}

var _ = gc.Suite(&unitServiceSuite{})

func (s *unitServiceSuite) TestGetUnitUUID(c *gc.C) {
	defer s.setupMocks(c).Finish()

	uuid := unittesting.GenUnitUUID(c)
	unitName := coreunit.Name("foo/666")
	s.state.EXPECT().GetUnitUUIDByName(gomock.Any(), unitName).Return(uuid, nil)

	u, err := s.service.GetUnitUUID(context.Background(), unitName)
	c.Assert(err, jc.ErrorIsNil)
	c.Check(u, gc.Equals, uuid)
}

func (s *unitServiceSuite) TestGetUnitUUIDErrors(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/666")
	s.state.EXPECT().GetUnitUUIDByName(gomock.Any(), unitName).Return("", applicationerrors.UnitNotFound)

	_, err := s.service.GetUnitUUID(context.Background(), unitName)
	c.Assert(err, jc.ErrorIs, applicationerrors.UnitNotFound)
}

type registerArgMatcher struct {
	c   *gc.C
	arg application.RegisterCAASUnitArg
}

func (m registerArgMatcher) Matches(x interface{}) bool {
	obtained, ok := x.(application.RegisterCAASUnitArg)
	if !ok {
		return false
	}

	m.c.Assert(obtained.PasswordHash, gc.Not(gc.Equals), "")
	obtained.PasswordHash = ""
	m.arg.PasswordHash = ""
	return reflect.DeepEqual(obtained, m.arg)
}

func (m registerArgMatcher) String() string {
	return pretty.Sprint(m.arg)
}

func (s *unitServiceSuite) TestRegisterCAASUnit(c *gc.C) {
	ctrl := s.setupMocksWithProvider(c,
		func(ctx context.Context) (Provider, error) {
			return s.provider, nil
		},
		func(ctx context.Context) (SupportedFeatureProvider, error) {
			return s.supportedFeaturesProvider, nil
		},
		func(ctx context.Context) (CAASApplicationProvider, error) {
			return s.caasApplicationProvider, nil
		})
	defer ctrl.Finish()

	app := NewMockApplication(ctrl)
	app.EXPECT().Units().Return([]caas.Unit{{
		Id:      "foo-666",
		Address: "10.6.6.6",
		Ports:   []string{"8080"},
		FilesystemInfo: []caas.FilesystemInfo{{
			Volume: caas.VolumeInfo{VolumeId: "vol-666"},
		}},
	}}, nil)
	s.caasApplicationProvider.EXPECT().Application("foo", caas.DeploymentStateful).Return(app)

	arg := application.RegisterCAASUnitArg{
		UnitName:                  "foo/666",
		PasswordHash:              "secret",
		ProviderID:                "foo-666",
		Address:                   ptr("10.6.6.6"),
		Ports:                     ptr([]string{"8080"}),
		OrderedScale:              true,
		OrderedId:                 666,
		StorageParentDir:          application.StorageParentDir,
		ObservedAttachedVolumeIDs: []string{"vol-666"},
	}
	s.state.EXPECT().RegisterCAASUnit(gomock.Any(), "foo", registerArgMatcher{arg: arg})

	p := application.RegisterCAASUnitParams{
		ApplicationName: "foo",
		ProviderID:      "foo-666",
	}
	unitName, password, err := s.service.RegisterCAASUnit(context.Background(), p)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(unitName.String(), gc.Equals, "foo/666")
	c.Assert(password, gc.Not(gc.Equals), "")
}

func (s *unitServiceSuite) TestRegisterCAASUnitMissingProviderID(c *gc.C) {
	defer s.setupMocks(c).Finish()

	p := application.RegisterCAASUnitParams{
		ApplicationName: "foo",
	}
	_, _, err := s.service.RegisterCAASUnit(context.Background(), p)
	c.Assert(err, gc.ErrorMatches, "provider id not valid")
}

func (s *unitServiceSuite) TestRegisterCAASUnitApplicationNoPods(c *gc.C) {
	ctrl := s.setupMocksWithProvider(c,
		func(ctx context.Context) (Provider, error) {
			return s.provider, nil
		},
		func(ctx context.Context) (SupportedFeatureProvider, error) {
			return s.supportedFeaturesProvider, nil
		},
		func(ctx context.Context) (CAASApplicationProvider, error) {
			return s.caasApplicationProvider, nil
		})
	defer ctrl.Finish()

	app := NewMockApplication(ctrl)
	app.EXPECT().Units().Return([]caas.Unit{}, nil)
	s.caasApplicationProvider.EXPECT().Application("foo", caas.DeploymentStateful).Return(app)

	p := application.RegisterCAASUnitParams{
		ApplicationName: "foo",
		ProviderID:      "foo-666",
	}
	_, _, err := s.service.RegisterCAASUnit(context.Background(), p)
	c.Assert(err, jc.ErrorIs, coreerrors.NotFound)
}

func (s *unitServiceSuite) TestUpdateCAASUnit(c *gc.C) {
	defer s.setupMocks(c).Finish()

	appID := applicationtesting.GenApplicationUUID(c)
	unitName := coreunit.Name("foo/666")
	now := time.Now()

	expected := application.UpdateCAASUnitParams{
		ProviderID: ptr("provider-id"),
		Address:    ptr("10.6.6.6"),
		Ports:      ptr([]string{"666"}),
		AgentStatus: ptr(status.StatusInfo[status.UnitAgentStatusType]{
			Status:  status.UnitAgentStatusAllocating,
			Message: "agent status",
			Data:    []byte(`{"foo":"bar"}`),
			Since:   ptr(now),
		}),
		WorkloadStatus: ptr(status.StatusInfo[status.WorkloadStatusType]{
			Status:  status.WorkloadStatusWaiting,
			Message: "workload status",
			Data:    []byte(`{"foo":"bar"}`),
			Since:   ptr(now),
		}),
		CloudContainerStatus: ptr(status.StatusInfo[status.CloudContainerStatusType]{
			Status:  status.CloudContainerStatusRunning,
			Message: "container status",
			Data:    []byte(`{"foo":"bar"}`),
			Since:   ptr(now),
		}),
	}

	params := UpdateCAASUnitParams{
		ProviderID: ptr("provider-id"),
		Address:    ptr("10.6.6.6"),
		Ports:      ptr([]string{"666"}),
		AgentStatus: ptr(corestatus.StatusInfo{
			Status:  corestatus.Allocating,
			Message: "agent status",
			Data:    map[string]interface{}{"foo": "bar"},
			Since:   ptr(now),
		}),
		WorkloadStatus: ptr(corestatus.StatusInfo{
			Status:  corestatus.Waiting,
			Message: "workload status",
			Data:    map[string]interface{}{"foo": "bar"},
			Since:   ptr(now),
		}),
		CloudContainerStatus: ptr(corestatus.StatusInfo{
			Status:  corestatus.Running,
			Message: "container status",
			Data:    map[string]interface{}{"foo": "bar"},
			Since:   ptr(now),
		}),
	}

	s.state.EXPECT().GetApplicationLife(gomock.Any(), "foo").Return(appID, life.Alive, nil)

	var unitArgs application.UpdateCAASUnitParams
	s.state.EXPECT().UpdateCAASUnit(gomock.Any(), unitName, gomock.Any()).DoAndReturn(func(_ context.Context, _ coreunit.Name, args application.UpdateCAASUnitParams) error {
		unitArgs = args
		return nil
	})

	err := s.service.UpdateCAASUnit(context.Background(), unitName, params)
	c.Assert(err, jc.ErrorIsNil)
	c.Check(unitArgs, jc.DeepEquals, expected)
}

func (s *unitServiceSuite) TestUpdateCAASUnitNotAlive(c *gc.C) {
	defer s.setupMocks(c).Finish()

	id := applicationtesting.GenApplicationUUID(c)
	s.state.EXPECT().GetApplicationLife(gomock.Any(), "foo").Return(id, life.Dying, nil)

	err := s.service.UpdateCAASUnit(context.Background(), coreunit.Name("foo/666"), UpdateCAASUnitParams{})
	c.Assert(err, jc.ErrorIs, applicationerrors.ApplicationNotAlive)
}

func (s *unitServiceSuite) TestGetUnitRefreshAttributes(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/666")
	attrs := application.UnitAttributes{
		Life: life.Alive,
	}
	s.state.EXPECT().GetUnitRefreshAttributes(gomock.Any(), unitName).Return(attrs, nil)

	refreshAttrs, err := s.service.GetUnitRefreshAttributes(context.Background(), unitName)
	c.Assert(err, jc.ErrorIsNil)
	c.Check(refreshAttrs, gc.Equals, attrs)
}

func (s *unitServiceSuite) TestGetUnitRefreshAttributesInvalidName(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("!!!")

	_, err := s.service.GetUnitRefreshAttributes(context.Background(), unitName)
	c.Assert(err, jc.ErrorIs, coreunit.InvalidUnitName)
}

func (s *unitServiceSuite) TestGetUnitRefreshAttributesError(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/666")
	attrs := application.UnitAttributes{
		Life: life.Alive,
	}
	s.state.EXPECT().GetUnitRefreshAttributes(gomock.Any(), unitName).Return(attrs, errors.Errorf("boom"))

	_, err := s.service.GetUnitRefreshAttributes(context.Background(), unitName)
	c.Assert(err, gc.ErrorMatches, "boom")
}

func (s *unitServiceSuite) TestGetAllUnitNames(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitNames := []coreunit.Name{"foo/666", "foo/667"}

	s.state.EXPECT().GetAllUnitNames(gomock.Any()).Return(unitNames, nil)

	names, err := s.service.GetAllUnitNames(context.Background())
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(names, jc.SameContents, unitNames)
}

func (s *unitServiceSuite) TestGetAllUnitNamesError(c *gc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().GetAllUnitNames(gomock.Any()).Return(nil, errors.Errorf("boom"))

	_, err := s.service.GetAllUnitNames(context.Background())
	c.Assert(err, gc.ErrorMatches, "boom")
}

func (s *unitServiceSuite) TestGetUnitNamesForApplication(c *gc.C) {
	defer s.setupMocks(c).Finish()

	appName := "foo"
	appID := applicationtesting.GenApplicationUUID(c)
	unitNames := []coreunit.Name{"foo/666", "foo/667"}

	s.state.EXPECT().GetApplicationIDByName(gomock.Any(), appName).Return(appID, nil)
	s.state.EXPECT().GetUnitNamesForApplication(gomock.Any(), appID).Return(unitNames, nil)

	names, err := s.service.GetUnitNamesForApplication(context.Background(), appName)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(names, jc.SameContents, unitNames)
}

func (s *unitServiceSuite) TestGetUnitNamesForApplicationNotFound(c *gc.C) {
	defer s.setupMocks(c).Finish()

	s.state.EXPECT().GetApplicationIDByName(gomock.Any(), "foo").Return("", applicationerrors.ApplicationNotFound)

	_, err := s.service.GetUnitNamesForApplication(context.Background(), "foo")
	c.Assert(err, jc.ErrorIs, applicationerrors.ApplicationNotFound)
}

func (s *unitServiceSuite) TestGetUnitNamesForApplicationDead(c *gc.C) {
	defer s.setupMocks(c).Finish()

	appName := "foo"
	appID := applicationtesting.GenApplicationUUID(c)

	s.state.EXPECT().GetApplicationIDByName(gomock.Any(), appName).Return(appID, nil)
	s.state.EXPECT().GetUnitNamesForApplication(gomock.Any(), appID).Return(nil, applicationerrors.ApplicationIsDead)

	_, err := s.service.GetUnitNamesForApplication(context.Background(), appName)
	c.Assert(err, jc.ErrorIs, applicationerrors.ApplicationIsDead)
}

func (s *unitServiceSuite) TestGetPublicAddressUnitNotFound(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID(""), errors.New("boom"))

	_, err := s.service.GetPublicAddress(context.Background(), unitName)
	c.Assert(err, gc.ErrorMatches, "boom")
}

func (s *unitServiceSuite) TestGetPublicAddressWithCloudServiceError(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(nil, errors.New("boom"))

	_, err := s.service.GetPublicAddress(context.Background(), unitName)
	c.Assert(err, gc.ErrorMatches, "boom")
}

func (s *unitServiceSuite) TestGetPublicAddressWithCloudServiceAddressesNotMatchingScopeCloudContainerError(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	nonMatchingScopeAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
	}

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(nonMatchingScopeAddrs, nil)
	s.state.EXPECT().GetUnitUUIDByName(gomock.Any(), coreunit.Name("foo/0")).Return(coreunit.UUID("foo-uuid"), nil)
	s.state.EXPECT().GetCloudContainerAddresses(gomock.Any(), coreunit.UUID("foo-uuid")).Return(nil, errors.New("boom"))

	_, err := s.service.GetPublicAddress(context.Background(), unitName)
	c.Assert(err, gc.ErrorMatches, "boom")
}

func (s *unitServiceSuite) TestGetPublicAddressNonMatchingAddresses(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	nonMatchingScopeServiceAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
	}
	nonMatchingScopeContainerAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.1.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.1.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
	}

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(nonMatchingScopeServiceAddrs, nil)
	s.state.EXPECT().GetUnitUUIDByName(gomock.Any(), coreunit.Name("foo/0")).Return(coreunit.UUID("foo-uuid"), nil)
	s.state.EXPECT().GetCloudContainerAddresses(gomock.Any(), coreunit.UUID("foo-uuid")).Return(nonMatchingScopeContainerAddrs, nil)

	_, err := s.service.GetPublicAddress(context.Background(), unitName)
	c.Assert(err, gc.ErrorMatches, "no public address.*")
}

func (s *unitServiceSuite) TestGetPublicAddressCloudService(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	matchingScopeAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopeCloudLocal,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopePublic,
			},
		},
	}

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(matchingScopeAddrs, nil)

	addrs, err := s.service.GetPublicAddress(context.Background(), unitName)
	c.Assert(err, jc.ErrorIsNil)
	// Since the second address is higher in hierarchy of scope match, it should
	// be returned.
	c.Check(addrs, gc.DeepEquals, matchingScopeAddrs[1])
}

func (s *unitServiceSuite) TestGetPublicAddressCloudContainer(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	matchingScopeAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopeCloudLocal,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopePublic,
			},
		},
	}

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(nil, nil)
	s.state.EXPECT().GetUnitUUIDByName(gomock.Any(), coreunit.Name("foo/0")).Return(coreunit.UUID("foo-uuid"), nil)
	s.state.EXPECT().GetCloudContainerAddresses(gomock.Any(), coreunit.UUID("foo-uuid")).Return(matchingScopeAddrs, nil)

	addrs, err := s.service.GetPublicAddress(context.Background(), unitName)
	c.Assert(err, jc.ErrorIsNil)
	// Since the second address is higher in hierarchy of scope match, it should
	// be returned.
	c.Check(addrs, gc.DeepEquals, matchingScopeAddrs[1])
}

func (s *unitServiceSuite) TestGetPrivateAddressUnitNotFound(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), errors.New("boom"))

	_, err := s.service.GetPrivateAddress(context.Background(), unitName)
	c.Assert(err, gc.ErrorMatches, "boom")
}

func (s *unitServiceSuite) TestGetPrivateAddressWithCloudServiceError(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(nil, errors.New("boom"))

	_, err := s.service.GetPrivateAddress(context.Background(), unitName)
	c.Assert(err, gc.ErrorMatches, "boom")
}

func (s *unitServiceSuite) TestGetPrivateAddressWithCloudServiceAddressesNotMatchingScopeCloudContainerError(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	nonMatchingScopeAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
	}

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(nonMatchingScopeAddrs, nil)
	s.state.EXPECT().GetUnitUUIDByName(gomock.Any(), coreunit.Name("foo/0")).Return(coreunit.UUID("foo-uuid"), nil)
	s.state.EXPECT().GetCloudContainerAddresses(gomock.Any(), coreunit.UUID("foo-uuid")).Return(nil, errors.New("boom"))

	_, err := s.service.GetPrivateAddress(context.Background(), unitName)
	c.Assert(err, gc.ErrorMatches, "boom")
}

func (s *unitServiceSuite) TestGetPrivateAddressNoAddresses(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	nonMatchingScopeServiceAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
	}

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(nonMatchingScopeServiceAddrs, nil)
	s.state.EXPECT().GetUnitUUIDByName(gomock.Any(), coreunit.Name("foo/0")).Return(coreunit.UUID("foo-uuid"), nil)
	s.state.EXPECT().GetCloudContainerAddresses(gomock.Any(), coreunit.UUID("foo-uuid")).Return(nil, nil)

	_, err := s.service.GetPrivateAddress(context.Background(), unitName)
	c.Assert(err, gc.ErrorMatches, "no local-cloud address.*")
}

func (s *unitServiceSuite) TestGetPrivateAddressNonMatchingAddresses(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	nonMatchingScopeServiceAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
	}
	nonMatchingScopeContainerAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.1.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.1.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopeMachineLocal,
			},
		},
	}

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(nonMatchingScopeServiceAddrs, nil)
	s.state.EXPECT().GetUnitUUIDByName(gomock.Any(), coreunit.Name("foo/0")).Return(coreunit.UUID("foo-uuid"), nil)
	s.state.EXPECT().GetCloudContainerAddresses(gomock.Any(), coreunit.UUID("foo-uuid")).Return(nonMatchingScopeContainerAddrs, nil)

	addr, err := s.service.GetPrivateAddress(context.Background(), unitName)
	c.Assert(err, jc.ErrorIsNil)
	// We always return the (first) container address even if it doesn't match
	// the scope.
	c.Assert(addr, gc.DeepEquals, nonMatchingScopeContainerAddrs[0])
}

func (s *unitServiceSuite) TestGetPrivateAddressCloudService(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	matchingScopeAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopePublic,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopeCloudLocal,
			},
		},
	}

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(matchingScopeAddrs, nil)

	addrs, err := s.service.GetPrivateAddress(context.Background(), unitName)
	c.Assert(err, jc.ErrorIsNil)
	// Since the second address is higher in hierarchy of scope match, it should
	// be returned.
	c.Check(addrs, gc.DeepEquals, matchingScopeAddrs[1])
}

func (s *unitServiceSuite) TestGetPrivateAddressCloudContainer(c *gc.C) {
	defer s.setupMocks(c).Finish()

	unitName := coreunit.Name("foo/0")

	matchingScopeAddrs := network.SpaceAddresses{
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.1",
				ConfigType: network.ConfigStatic,
				Type:       network.IPv4Address,
				Scope:      network.ScopePublic,
			},
		},
		{
			SpaceID: network.AlphaSpaceId,
			MachineAddress: network.MachineAddress{
				Value:      "10.0.0.2",
				ConfigType: network.ConfigDHCP,
				Type:       network.IPv6Address,
				Scope:      network.ScopeCloudLocal,
			},
		},
	}

	s.state.EXPECT().GetApplicationIDByUnitName(gomock.Any(), unitName).Return(coreapplication.ID("foo"), nil)
	s.state.EXPECT().GetCloudServiceAddresses(gomock.Any(), coreapplication.ID("foo")).Return(nil, nil)
	s.state.EXPECT().GetUnitUUIDByName(gomock.Any(), coreunit.Name("foo/0")).Return(coreunit.UUID("foo-uuid"), nil)
	s.state.EXPECT().GetCloudContainerAddresses(gomock.Any(), coreunit.UUID("foo-uuid")).Return(matchingScopeAddrs, nil)

	addrs, err := s.service.GetPrivateAddress(context.Background(), unitName)
	c.Assert(err, jc.ErrorIsNil)
	// Since the second address is higher in hierarchy of scope match, it should
	// be returned.
	c.Check(addrs, gc.DeepEquals, matchingScopeAddrs[1])
}
