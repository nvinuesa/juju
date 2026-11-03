// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	stdtesting "testing"

	"github.com/juju/tc"

	schematesting "github.com/juju/juju/domain/schema/testing"
)

type loadSuite struct {
	schematesting.ControllerModelSuite
}

func TestLoadSuite(t *stdtesting.T) {
	tc.Run(t, &loadSuite{})
}
