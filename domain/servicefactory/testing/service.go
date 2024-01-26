// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package testing

import (
	autocertcacheservice "github.com/juju/juju/domain/autocert/service"
	cloudservice "github.com/juju/juju/domain/cloud/service"
	controllerconfigservice "github.com/juju/juju/domain/controllerconfig/service"
	controllernodeservice "github.com/juju/juju/domain/controllernode/service"
	credentialservice "github.com/juju/juju/domain/credential/service"
	externalcontrollerservice "github.com/juju/juju/domain/externalcontroller/service"
	flagservice "github.com/juju/juju/domain/flag/service"
	machineservice "github.com/juju/juju/domain/machine/service"
	modelservice "github.com/juju/juju/domain/model/service"
	modelconfigservice "github.com/juju/juju/domain/modelconfig/service"
	modeldefaultsservice "github.com/juju/juju/domain/modeldefaults/service"
	modelmanagerservice "github.com/juju/juju/domain/modelmanager/service"
	networkservice "github.com/juju/juju/domain/network/service"
	objectstoreservice "github.com/juju/juju/domain/objectstore/service"
	upgradeservice "github.com/juju/juju/domain/upgrade/service"
	userservice "github.com/juju/juju/domain/user/service"
	"github.com/juju/juju/internal/servicefactory"
)

// TestingServiceFactory provides access to the services required by the apiserver.
type TestingServiceFactory struct {
	machineServiceGetter func() *machineservice.Service
}

// NewTestingServiceFactory returns a new registry which uses the provided controllerDB
// function to obtain a controller database.
func NewTestingServiceFactory() *TestingServiceFactory {
	return &TestingServiceFactory{}
}

// AutocertCache returns the autocert cache service.
func (s *TestingServiceFactory) AutocertCache() *autocertcacheservice.Service {
	return nil
}

// Config returns the model config service.
func (s *TestingServiceFactory) Config(_ modelconfigservice.ModelDefaultsProvider) *modelconfigservice.Service {
	return nil
}

// ControllerConfig returns the controller configuration service.
func (s *TestingServiceFactory) ControllerConfig() *controllerconfigservice.Service {
	return nil
}

// ControllerNode returns the controller node service.
func (s *TestingServiceFactory) ControllerNode() *controllernodeservice.Service {
	return nil
}

// Model returns the model service.
func (s *TestingServiceFactory) Model() *modelservice.Service {
	return nil
}

// ModelDefaults returns the model defaults service.
func (s *TestingServiceFactory) ModelDefaults() *modeldefaultsservice.Service {
	return nil
}

// ModelManager returns the model manager service.
func (s *TestingServiceFactory) ModelManager() *modelmanagerservice.Service {
	return nil
}

// ExternalController returns the external controller service.
func (s *TestingServiceFactory) ExternalController() *externalcontrollerservice.Service {
	return nil
}

// Credential returns the credential service.
func (s *TestingServiceFactory) Credential() *credentialservice.Service {
	return nil
}

// Cloud returns the cloud service.
func (s *TestingServiceFactory) Cloud() *cloudservice.Service {
	return nil
}

// Upgrade returns the upgrade service.
func (s *TestingServiceFactory) Upgrade() *upgradeservice.Service {
	return nil
}

// AgentObjectStore returns the agent object store service.
func (s *TestingServiceFactory) AgentObjectStore() *objectstoreservice.Service {
	return nil
}

// ObjectStore returns the object store service.
func (s *TestingServiceFactory) ObjectStore() *objectstoreservice.Service {
	return nil
}

// Flag returns the flag service.
func (s *TestingServiceFactory) Flag() *flagservice.Service {
	return nil
}

// User returns the user service.
func (s *TestingServiceFactory) User() *userservice.Service {
	return nil
}

// Machine returns the machine service.
func (s *TestingServiceFactory) Machine() *machineservice.Service {
	if s.machineServiceGetter == nil {
		return nil
	}
	return s.machineServiceGetter()
}

// Space returns the space service.
func (s *TestingServiceFactory) Space() *networkservice.SpaceService {
	return nil
}

// FactoryForModel returns a service factory for the given model uuid.
// This will late bind the model service factory to the actual service factory.
func (s *TestingServiceFactory) FactoryForModel(modelUUID string) servicefactory.ServiceFactory {
	return s
}

// WithMachineService returns a service factory which gets its machine service
// using the supplied getter.
func (s *TestingServiceFactory) WithMachineService(getter func() *machineservice.Service) *TestingServiceFactory {
	s.machineServiceGetter = getter
	return s
}
