// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provisioner

import (
	"context"

	jc "github.com/juju/testing/checkers"
	"go.uber.org/mock/gomock"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/core/charm/testing"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/envcontext"
	environtesting "github.com/juju/juju/environs/testing"
	"github.com/juju/juju/internal/charm"
	loggertesting "github.com/juju/juju/internal/logger/testing"
	coretesting "github.com/juju/juju/internal/testing"
	"github.com/juju/juju/rpc/params"
)

// TODO(jam): 2017-02-15 We seem to be lacking most of direct unit tests around ProcessOneContainer
// Some of the use cases we need to be testing are:
// 1) Provider can allocate addresses, should result in a container with
//    addresses requested from the provider, and 'static' configuration on those
//    devices.
// 2) Provider cannot allocate addresses, currently this should make us use
//    'lxdbr0' and DHCP allocated addresses.
// 3) Provider could allocate DHCP based addresses on the host device, which would let us
//    use a bridge on the device and DHCP. (Currently not supported, but desirable for
//    vSphere and Manual and probably LXD providers.)
// Addition (manadart 2018-10-09): To begin accommodating the deficiencies noted
// above, the new suite below uses mocks for tests ill-suited to the dummy
// provider. We could reasonably re-write the tests above over time to use the
// new suite.
// Addition (tlm 2024-08-27): The old "integration" tests using apiserver suite
// have been put into their own file. New tests should be added here using
// mocks.

type provisionerMockSuite struct {
	coretesting.BaseSuite

	environ            *environtesting.MockNetworkingEnviron
	policy             *MockBridgePolicy
	host               *MockMachine
	container          *MockMachine
	applicationService *MockApplicationService
	device             *MockLinkLayerDevice
	parentDevice       *MockLinkLayerDevice

	unit        *MockUnit
	application *MockApplication
}

var _ = gc.Suite(&provisionerMockSuite{})

// Even when the provider supports container addresses, manually provisioned
// machines should fall back to DHCP.
func (s *provisionerMockSuite) TestManuallyProvisionedHostsUseDHCPForContainers(c *gc.C) {
	defer s.setup(c).Finish()

	s.expectManuallyProvisionedHostsUseDHCPForContainers()

	res := params.MachineNetworkConfigResults{
		Results: []params.MachineNetworkConfigResult{{}},
	}
	ctx := prepareOrGetHandler{result: res, maintain: false, logger: loggertesting.WrapCheckLog(c)}
	callCtx := envcontext.WithoutCredentialInvalidator(context.Background())

	// ProviderCallContext is not required by this logical path and can be nil
	err := ctx.ProcessOneContainer(context.Background(), s.environ, callCtx, s.policy, 0, s.host, s.container, instance.Id(""), instance.Id(""), nil)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(res.Results[0].Config, gc.HasLen, 1)

	cfg := res.Results[0].Config[0]
	c.Check(cfg.ConfigType, gc.Equals, "dhcp")
	c.Check(cfg.ProviderSubnetId, gc.Equals, "")
	c.Check(cfg.VLANTag, gc.Equals, 0)
}

func (s *provisionerMockSuite) expectManuallyProvisionedHostsUseDHCPForContainers() {
	s.expectNetworkingEnviron()

	cExp := s.container.EXPECT()
	// cExp.InstanceId().Return(instance.UnknownId, errors.NotProvisionedf("idk-lol"))

	s.policy.EXPECT().PopulateContainerLinkLayerDevices(s.host, s.container, false).Return(
		network.InterfaceInfos{
			{
				InterfaceName: "eth0",
				ConfigType:    network.ConfigDHCP,
			},
		}, nil)

	cExp.Id().Return("lxd/0").AnyTimes()

	hExp := s.host.EXPECT()
	// Crucial behavioural trait. Set false to test failure, whereupon the
	// PopulateContainerLinkLayerDevices expectation will not be satisfied.
	hExp.IsManual().Return(true, nil)
	// hExp.InstanceId().Return(instance.Id("manual:10.0.0.66"), nil)
}

// expectNetworkingEnviron stubs an environ that supports container networking.
func (s *provisionerMockSuite) expectNetworkingEnviron() {
	eExp := s.environ.EXPECT()
	eExp.Config().Return(&config.Config{}).AnyTimes()
	eExp.SupportsContainerAddresses(gomock.Any()).Return(true, nil).AnyTimes()
}

func (s *provisionerMockSuite) TestContainerAlreadyProvisionedError(c *gc.C) {
	defer s.setup(c).Finish()

	exp := s.container.EXPECT()
	exp.Id().Return("0/lxd/0")

	res := params.MachineNetworkConfigResults{
		Results: []params.MachineNetworkConfigResult{{}},
	}
	ctx := prepareOrGetHandler{
		result:   res,
		maintain: true,
		logger:   loggertesting.WrapCheckLog(c),
	}
	callCtx := envcontext.WithoutCredentialInvalidator(context.Background())

	// ProviderCallContext and BridgePolicy are not
	// required by this logical path and can be nil.
	err := ctx.ProcessOneContainer(context.Background(), s.environ, callCtx, nil, 0, s.host, s.container, instance.Id(""), instance.Id("juju-8ebd6c-0"), nil)
	c.Assert(err, gc.ErrorMatches, `container "0/lxd/0" already provisioned as "juju-8ebd6c-0"`)
}

// TODO: this is not a great test name, this test does not even call
//
//	ProvisionerAPI.GetContainerProfileInfo.
func (s *provisionerMockSuite) TestGetContainerProfileInfo(c *gc.C) {
	ctrl := s.setup(c)
	defer ctrl.Finish()
	s.expectCharmLXDProfiles(ctrl)

	s.application.EXPECT().Name().Return("application")

	charmUUID := testing.GenCharmID(c)
	s.applicationService.EXPECT().GetCharmIDByApplicationName(gomock.Any(), "application").Return(charmUUID, nil)
	s.applicationService.EXPECT().GetCharmLXDProfile(gomock.Any(), charmUUID).Return(charm.LXDProfile{
		Config: map[string]string{
			"security.nesting":    "true",
			"security.privileged": "true",
		},
	}, 3, nil)

	res := params.ContainerProfileResults{
		Results: []params.ContainerProfileResult{{}},
	}
	ctx := containerProfileHandler{
		applicationService: s.applicationService,
		result:             res,
		modelName:          "testme",
		logger:             loggertesting.WrapCheckLog(c),
	}
	callCtx := envcontext.WithoutCredentialInvalidator(context.Background())

	// ProviderCallContext and BridgePolicy are not
	// required by this logical path and can be nil.
	err := ctx.ProcessOneContainer(context.Background(), s.environ, callCtx, nil, 0, s.host, s.container, instance.Id(""), instance.Id(""), nil)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(res.Results, gc.HasLen, 1)
	c.Assert(res.Results[0].Error, gc.IsNil)
	c.Assert(res.Results[0].LXDProfiles, gc.HasLen, 1)
	profile := res.Results[0].LXDProfiles[0]
	c.Check(profile.Name, gc.Equals, "juju-testme-application-3")
	c.Check(profile.Profile.Config, gc.DeepEquals,
		map[string]string{
			"security.nesting":    "true",
			"security.privileged": "true",
		},
	)
}

func (s *provisionerMockSuite) TestGetContainerProfileInfoNoProfile(c *gc.C) {
	ctrl := s.setup(c)
	defer ctrl.Finish()
	s.expectCharmLXDProfiles(ctrl)

	s.unit.EXPECT().Name().Return("application/0")
	s.application.EXPECT().Name().Return("application")

	charmUUID := testing.GenCharmID(c)
	s.applicationService.EXPECT().GetCharmIDByApplicationName(gomock.Any(), "application").Return(charmUUID, nil)
	s.applicationService.EXPECT().GetCharmLXDProfile(gomock.Any(), charmUUID).Return(charm.LXDProfile{}, -1, nil)

	res := params.ContainerProfileResults{
		Results: []params.ContainerProfileResult{{}},
	}
	ctx := containerProfileHandler{
		applicationService: s.applicationService,
		result:             res,
		modelName:          "testme",
		logger:             loggertesting.WrapCheckLog(c),
	}
	callCtx := envcontext.WithoutCredentialInvalidator(context.Background())

	// ProviderCallContext and BridgePolicy are not
	// required by this logical path and can be nil.
	err := ctx.ProcessOneContainer(context.Background(), s.environ, callCtx, nil, 0, s.host, s.container, instance.Id(""), instance.Id(""), nil)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(res.Results, gc.HasLen, 1)
	c.Assert(res.Results[0].Error, gc.IsNil)
	c.Assert(res.Results[0].LXDProfiles, gc.HasLen, 0)
}

func (s *provisionerMockSuite) expectCharmLXDProfiles(ctrl *gomock.Controller) {
	s.container.EXPECT().Units().Return([]Unit{s.unit}, nil)
	s.unit.EXPECT().Application().Return(s.application, nil)
}

func (s *provisionerMockSuite) setup(c *gc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)

	s.environ = environtesting.NewMockNetworkingEnviron(ctrl)
	s.policy = NewMockBridgePolicy(ctrl)
	s.host = NewMockMachine(ctrl)
	s.container = NewMockMachine(ctrl)
	s.device = NewMockLinkLayerDevice(ctrl)
	s.parentDevice = NewMockLinkLayerDevice(ctrl)

	s.applicationService = NewMockApplicationService(ctrl)
	s.application = NewMockApplication(ctrl)
	s.unit = NewMockUnit(ctrl)

	return ctrl
}
