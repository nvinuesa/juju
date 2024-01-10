// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package servicefactory

import (
	"github.com/juju/juju/core/changestream"
	"github.com/juju/juju/domain"
	modelconfigservice "github.com/juju/juju/domain/modelconfig/service"
	modelconfigstate "github.com/juju/juju/domain/modelconfig/state"
	networkservice "github.com/juju/juju/domain/network/service"
	networkstate "github.com/juju/juju/domain/network/state"
	objectstoreservice "github.com/juju/juju/domain/objectstore/service"
	objectstorestate "github.com/juju/juju/domain/objectstore/state"
)

// ModelFactory provides access to the services required by the apiserver.
type ModelFactory struct {
	logger  Logger
	modelDB changestream.WatchableDBFactory
}

// NewModelFactory returns a new registry which uses the provided modelDB
// function to obtain a model database.
func NewModelFactory(
	modelDB changestream.WatchableDBFactory,
	logger Logger,
) *ModelFactory {
	return &ModelFactory{
		logger:  logger,
		modelDB: modelDB,
	}
}

// Config returns the model's configuration service. A ModelDefaultsProvider
// needs to be supplied for the model config service. The provider can be
// obtained from the controller service factory model defaults service.
func (s *ModelFactory) Config(
	defaultsProvider modelconfigservice.ModelDefaultsProvider,
) *modelconfigservice.Service {
	return modelconfigservice.NewService(
		defaultsProvider,
		modelconfigstate.NewState(changestream.NewTxnRunnerFactory(s.modelDB)),
		domain.NewWatcherFactory(s.modelDB, s.logger.Child("modelconfig")),
	)
}

// ObjectStore returns the model's object store service.
func (s *ModelFactory) ObjectStore() *objectstoreservice.Service {
	return objectstoreservice.NewService(
		objectstorestate.NewState(changestream.NewTxnRunnerFactory(s.modelDB)),
		domain.NewWatcherFactory(
			s.modelDB,
			s.logger.Child("objectstore"),
		),
	)
}

// Space returns the space service.
func (s *ModelFactory) Space() *networkservice.SpaceService {
	return networkservice.NewSpaceService(
		networkstate.NewState(changestream.NewTxnRunnerFactory(s.modelDB)),
		s.logger.Child("network"),
	)
}

// Subnet returns the subnet service.
func (s *ModelFactory) Subnet() *networkservice.SubnetService {
	return networkservice.NewSubnetService(
		networkstate.NewState(changestream.NewTxnRunnerFactory(s.modelDB)),
	)
}
