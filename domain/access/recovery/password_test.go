// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	stdtesting "testing"

	"github.com/juju/tc"

	"github.com/juju/juju/core/user"
	usererrors "github.com/juju/juju/domain/access/errors"
	accessrecovery "github.com/juju/juju/domain/access/recovery"
	"github.com/juju/juju/internal/auth"
)

type passwordSuite struct{}

func TestPasswordSuite(t *stdtesting.T) {
	tc.Run(t, &passwordSuite{})
}

// TestSetUserPasswordRejectsZeroName checks that recovery never reaches the
// database without a user to rebind.
func (s *passwordSuite) TestSetUserPasswordRejectsZeroName(c *tc.C) {
	err := accessrecovery.SetUserPassword(c.Context(), nil, user.Name{}, auth.NewPassword("secret"))
	c.Check(err, tc.ErrorIs, usererrors.UserNameNotValid)
}
