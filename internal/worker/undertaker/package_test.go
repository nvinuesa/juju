// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package undertaker_test

import (
	stdtesting "testing"

	"github.com/juju/tc"
)

//go:generate go run go.uber.org/mock/mockgen -typed -package undertaker_test -destination facade_mock_test.go github.com/juju/juju/internal/worker/undertaker Facade

func TestPackage(t *stdtesting.T) {
	tc.TestingT(t)
}
