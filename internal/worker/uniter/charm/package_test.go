// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package charm_test

import (
	stdtesting "testing"

	gc "gopkg.in/check.v1"
)

//go:generate go run go.uber.org/mock/mockgen -typed -package mocks -destination mocks/mocks.go github.com/juju/juju/internal/worker/uniter/charm BundleReader,BundleInfo,Bundle

func TestPackage(t *stdtesting.T) {
	gc.TestingT(t)
}
