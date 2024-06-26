// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package unitassigner

import (
	"github.com/juju/errors"
	"github.com/juju/worker/v4"
	"github.com/juju/worker/v4/dependency"

	"github.com/juju/juju/agent/engine"
	"github.com/juju/juju/api/agent/unitassigner"
	"github.com/juju/juju/api/base"
	"github.com/juju/juju/core/logger"
)

// ManifoldConfig describes the resources used by a unitassigner worker.
type ManifoldConfig struct {
	APICallerName string
	Logger        logger.Logger
}

// Manifold returns a Manifold that runs a unitassigner worker.
func Manifold(config ManifoldConfig) dependency.Manifold {
	return engine.APIManifold(
		engine.APIManifoldConfig{
			APICallerName: config.APICallerName,
		},
		config.start,
	)
}

// start returns a unitassigner worker using the supplied APICaller.
func (c *ManifoldConfig) start(apiCaller base.APICaller) (worker.Worker, error) {
	facade := unitassigner.New(apiCaller)
	worker, err := New(facade, c.Logger)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return worker, nil
}
