// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package environs_test

import (
	stdtesting "testing"

	"github.com/juju/tc"

	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/errors"
)

type bootstrapInstanceErrorSuite struct{}

func TestBootstrapInstanceErrorSuite(t *stdtesting.T) {
	tc.Run(t, &bootstrapInstanceErrorSuite{})
}

func (s *bootstrapInstanceErrorSuite) TestBootstrapInstanceIDExtractsWrapped(c *tc.C) {
	err := errors.Errorf("failed to bootstrap model: %w", &environs.BootstrapInstanceError{
		InstanceID: "i-abc",
		Err:        errors.New("load failed"),
	})

	id, ok := environs.BootstrapInstanceID(err)
	c.Assert(ok, tc.IsTrue)
	c.Check(id, tc.Equals, instance.Id("i-abc"))
	c.Check(err, tc.ErrorMatches, `failed to bootstrap model: bootstrap failed after instance "i-abc" started: load failed`)
}

func (s *bootstrapInstanceErrorSuite) TestBootstrapInstanceIDAbsent(c *tc.C) {
	id, ok := environs.BootstrapInstanceID(errors.New("preflight refused"))
	c.Assert(ok, tc.IsFalse)
	c.Check(id, tc.Equals, instance.Id(""))
}
