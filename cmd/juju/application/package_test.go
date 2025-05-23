// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package application_test

import (
	stdtesting "testing"

	"github.com/juju/tc"
)

//go:generate go run go.uber.org/mock/mockgen -typed -package mocks -destination mocks/applicationapi_mock.go github.com/juju/juju/cmd/juju/application ApplicationAPI,RemoveApplicationAPI
//go:generate go run go.uber.org/mock/mockgen -typed -package mocks -destination mocks/modelconfigapi_mock.go github.com/juju/juju/cmd/juju/application ModelConfigClient
//go:generate go run go.uber.org/mock/mockgen -typed -package mocks -destination mocks/deployer_mock.go github.com/juju/juju/cmd/juju/application/deployer Deployer,DeployerFactory
//go:generate go run go.uber.org/mock/mockgen -typed -package mocks -destination mocks/expose_mock.go github.com/juju/juju/cmd/juju/application ApplicationExposeAPI

func TestPackage(t *stdtesting.T) {
	tc.TestingT(t)
}
