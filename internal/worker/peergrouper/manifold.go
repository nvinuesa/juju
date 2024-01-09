// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package peergrouper

import (
	"github.com/juju/clock"
	"github.com/juju/errors"
	"github.com/juju/worker/v3"
	"github.com/juju/worker/v3/dependency"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/core/database"
	networkservice "github.com/juju/juju/domain/network/service"
	networkstate "github.com/juju/juju/domain/network/state"
	"github.com/juju/juju/internal/worker/common"
	workerstate "github.com/juju/juju/internal/worker/state"
	"github.com/juju/juju/state"
)

// Logger represents the methods used by the worker to log details.
type Logger interface {
	Debugf(string, ...interface{})
}

// ManifoldConfig holds the information necessary to run a peergrouper
// in a dependency.Engine.
type ManifoldConfig struct {
	AgentName      string
	ClockName      string
	StateName      string
	Hub            Hub
	DBAccessorName string

	PrometheusRegisterer prometheus.Registerer
	NewWorker            func(Config) (worker.Worker, error)
	NewSpaceGetter       func(database.DBGetter, Logger) SpaceGetter
}

// Validate validates the manifold configuration.
func (config ManifoldConfig) Validate() error {
	if config.AgentName == "" {
		return errors.NotValidf("empty AgentName")
	}
	if config.ClockName == "" {
		return errors.NotValidf("empty ClockName")
	}
	if config.StateName == "" {
		return errors.NotValidf("empty StateName")
	}
	if config.Hub == nil {
		return errors.NotValidf("nil Hub")
	}
	if config.DBAccessorName == "" {
		return errors.NotValidf("empty DBAccessorName")
	}
	if config.PrometheusRegisterer == nil {
		return errors.NotValidf("nil PrometheusRegisterer")
	}
	if config.NewWorker == nil {
		return errors.NotValidf("nil NewWorker")
	}
	if config.NewSpaceGetter == nil {
		return errors.NotValidf("nil NewSpaceGetter")
	}
	return nil
}

// Manifold returns a dependency.Manifold that will run a peergrouper.
func Manifold(config ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{
			config.AgentName,
			config.ClockName,
			config.StateName,
		},
		Start: config.start,
	}
}

// start is a method on ManifoldConfig because it's more readable than a closure.
func (config ManifoldConfig) start(context dependency.Context) (worker.Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}

	var agent agent.Agent
	if err := context.Get(config.AgentName, &agent); err != nil {
		return nil, errors.Trace(err)
	}

	var clock clock.Clock
	if err := context.Get(config.ClockName, &clock); err != nil {
		return nil, errors.Trace(err)
	}

	var stTracker workerstate.StateTracker
	if err := context.Get(config.StateName, &stTracker); err != nil {
		return nil, errors.Trace(err)
	}
	_, st, err := stTracker.Use()
	if err != nil {
		return nil, errors.Trace(err)
	}

	mongoSession := st.MongoSession()
	agentConfig := agent.CurrentConfig()
	controllerConfig, err := st.ControllerConfig()
	if err != nil {
		_ = stTracker.Done()
		return nil, errors.Trace(err)
	}
	model, err := st.Model()
	if err != nil {
		_ = stTracker.Done()
		return nil, errors.Trace(err)
	}
	supportsHA := model.Type() != state.ModelTypeCAAS

	var dbGetter database.DBGetter
	if err := context.Get(config.DBAccessorName, &dbGetter); err != nil {
		return nil, errors.Trace(err)
	}
	spaceGetter := config.NewSpaceGetter(dbGetter, logger)

	w, err := config.NewWorker(Config{
		State:                StateShim{st},
		MongoSession:         MongoSessionShim{mongoSession},
		APIHostPortsSetter:   &CachingAPIHostPortsSetter{APIHostPortsSetter: st},
		Clock:                clock,
		Hub:                  config.Hub,
		MongoPort:            controllerConfig.StatePort(),
		APIPort:              controllerConfig.APIPort(),
		ControllerAPIPort:    controllerConfig.ControllerAPIPort(),
		SupportsHA:           supportsHA,
		PrometheusRegisterer: config.PrometheusRegisterer,
		SpaceGetter:          spaceGetter,
		// On machine models, the controller id is the same as the machine/agent id.
		// TODO(wallyworld) - revisit when we add HA to k8s.
		ControllerId: agentConfig.Tag().Id,
	})
	if err != nil {
		_ = stTracker.Done()
		return nil, errors.Trace(err)
	}
	return common.NewCleanupWorker(w, func() { _ = stTracker.Done() }), nil
}

// NewSpaceGetter returns a new lease store based on the input config.
func NewSpaceGetter(dbGetter database.DBGetter, logger Logger) SpaceGetter {
	factory := database.NewTxnRunnerFactoryForNamespace(dbGetter.GetDB, database.ControllerNS)
	return networkservice.NewSpaceService(networkstate.NewState(factory), logger)
}
