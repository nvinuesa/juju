// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package applicationoffers_test

import (
	"github.com/juju/names/v6"
	jtesting "github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"go.uber.org/mock/gomock"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/common/crossmodel"
	"github.com/juju/juju/apiserver/facades/client/applicationoffers"
	"github.com/juju/juju/apiserver/testing"
	jujucrossmodel "github.com/juju/juju/core/crossmodel"
	coremodel "github.com/juju/juju/core/model"
	modeltesting "github.com/juju/juju/core/model/testing"
	coreuser "github.com/juju/juju/core/user"
	"github.com/juju/juju/domain/relation"
	"github.com/juju/juju/internal/charm"
	"github.com/juju/juju/internal/uuid"
)

const (
	addOffersBackendCall   = "addOffersCall"
	updateOfferBackendCall = "updateOfferCall"
	listOffersBackendCall  = "listOffersCall"
)

type baseSuite struct {
	jtesting.IsolationSuite

	authorizer *testing.FakeAuthorizer

	mockState                     *mockState
	mockStatePool                 *mockStatePool
	bakery                        *mockBakeryService
	authContext                   *crossmodel.AuthContext
	applicationOffers             *stubApplicationOffers
	modelUUID                     coremodel.UUID
	mockAccessService             *MockAccessService
	mockModelDomainServicesGetter *MockModelDomainServicesGetter
	mockModelDomainServices       *MockModelDomainServices
	mockApplicationService        *MockApplicationService
	mockModelService              *MockModelService
}

func (s *baseSuite) SetUpTest(c *gc.C) {
	s.IsolationSuite.SetUpTest(c)
	s.authorizer = &testing.FakeAuthorizer{
		Tag:      names.NewUserTag("read"),
		AdminTag: names.NewUserTag("admin"),
	}

	s.modelUUID = modeltesting.GenModelUUID(c)
	s.mockState = &mockState{
		applicationOffers: make(map[string]jujucrossmodel.ApplicationOffer),
		relations:         make(map[string]crossmodel.Relation),
		relationNetworks:  &mockRelationNetworks{},
	}
	s.mockStatePool = &mockStatePool{map[string]applicationoffers.Backend{s.modelUUID.String(): s.mockState}}
}

func (s *baseSuite) setupMocks(c *gc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)
	s.mockAccessService = NewMockAccessService(ctrl)
	s.mockApplicationService = NewMockApplicationService(ctrl)
	s.mockModelDomainServicesGetter = NewMockModelDomainServicesGetter(ctrl)
	s.mockModelDomainServices = NewMockModelDomainServices(ctrl)
	s.mockModelService = NewMockModelService(ctrl)
	return ctrl
}

func (s *baseSuite) addApplication(c *gc.C, name string) jujucrossmodel.ApplicationOffer {
	return jujucrossmodel.ApplicationOffer{
		OfferName:              "offer-" + name,
		OfferUUID:              "offer-" + name + "-uuid",
		ApplicationName:        name,
		Endpoints:              map[string]charm.Relation{"db": {Name: "db"}},
		ApplicationDescription: "applicaion description",
	}
}

func (s *baseSuite) setupOffers(c *gc.C, filterAppName string, filterWithEndpoints bool) string {
	offerUUID := uuid.MustNewUUID().String()
	s.setupOffersForUUID(c, offerUUID, filterAppName, filterWithEndpoints)
	return offerUUID
}

func (s *baseSuite) setupOffersForUUID(c *gc.C, offerUUID, filterAppName string, filterWithEndpoints bool) {
	applicationName := "test"
	offerName := "hosted-db2"

	anOffer := jujucrossmodel.ApplicationOffer{
		OfferName:              offerName,
		OfferUUID:              offerUUID,
		ApplicationName:        applicationName,
		ApplicationDescription: "description",
		Endpoints: map[string]charm.Relation{
			"db": {
				Name: "db2",
			},
		},
	}

	s.applicationOffers.listOffers = func(filters ...jujucrossmodel.ApplicationOfferFilter) ([]jujucrossmodel.ApplicationOffer, error) {
		c.Assert(filters, gc.HasLen, 1)
		expectedFilter := jujucrossmodel.ApplicationOfferFilter{
			OfferName:       offerName,
			ApplicationName: filterAppName,
		}
		if filterWithEndpoints {
			expectedFilter.Endpoints = []jujucrossmodel.EndpointFilterTerm{{
				Interface: "db2",
			}}
		}
		c.Assert(filters[0], jc.DeepEquals, expectedFilter)
		return []jujucrossmodel.ApplicationOffer{anOffer}, nil
	}
	s.mockState.applications = map[string]crossmodel.Application{
		"test": &mockApplication{
			name:     "test",
			curl:     "ch:db2-2",
			bindings: map[string]string{"db2": "myspace"}, // myspace
		},
	}
	userFred, err := coreuser.NewName("fred@external")
	c.Assert(err, jc.ErrorIsNil)

	s.mockModelService.EXPECT().ListAllModels(gomock.Any()).Return(
		[]coremodel.Model{
			{
				Name:      "prod",
				OwnerName: userFred,
				UUID:      s.modelUUID,
				ModelType: coremodel.IAAS,
			},
		}, nil,
	).AnyTimes()

	s.mockModelService.EXPECT().GetModelByNameAndOwner(gomock.Any(), "prod", userFred).Return(
		coremodel.Model{
			Name:      "prod",
			OwnerName: userFred,
			UUID:      s.modelUUID,
			ModelType: coremodel.IAAS,
		}, nil,
	).AnyTimes()

	s.mockState.relations["hosted-db2:db wordpress:db"] = &mockRelation{
		id: 1,
		endpoint: relation.Endpoint{
			ApplicationName: "test",
			Relation: charm.Relation{
				Name:      "db",
				Interface: "db2",
				Role:      "provider",
			},
		},
	}
	s.mockState.connections = []applicationoffers.OfferConnection{
		&mockOfferConnection{
			username:    "fred@external",
			modelUUID:   s.modelUUID.String(),
			relationKey: "hosted-db2:db wordpress:db",
			relationId:  1,
		},
	}
}
