// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore_test

import (
	stdtesting "testing"

	"github.com/juju/tc"
)

func TestValidateSuite(t *stdtesting.T) {
	tc.Run(t, &validateSuite{})
}
