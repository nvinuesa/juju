// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package relation

import (
	"github.com/juju/tc"

	coreerrors "github.com/juju/juju/core/errors"
	"github.com/juju/juju/internal/testhelpers"
	"github.com/juju/juju/internal/uuid"
)

type relationUUIDSuite struct {
	testhelpers.IsolationSuite
}

var _ = tc.Suite(&relationUUIDSuite{})

func (*relationUUIDSuite) TestUUIDValidate(c *tc.C) {
	// Test that the uuid.Validate method succeeds and
	// fails as expected.
	tests := []struct {
		uuid string
		err  error
	}{
		{
			uuid: "",
			err:  coreerrors.NotValid,
		},
		{
			uuid: "invalid",
			err:  coreerrors.NotValid,
		},
		{
			uuid: uuid.MustNewUUID().String(),
		},
	}

	for i, test := range tests {
		c.Logf("test %d: %q", i, test.uuid)
		err := UUID(test.uuid).Validate()

		if test.err == nil {
			c.Check(err, tc.IsNil)
			continue
		}

		c.Check(err, tc.ErrorIs, test.err)
	}
}

type relationUnitUUIDSuite struct {
	testhelpers.IsolationSuite
}

var _ = tc.Suite(&relationUnitUUIDSuite{})

func (*relationUnitUUIDSuite) TestUUIDValidate(c *tc.C) {
	// Test that the uuid.Validate method succeeds and
	// fails as expected.
	tests := []struct {
		uuid string
		err  error
	}{
		{
			uuid: "",
			err:  coreerrors.NotValid,
		},
		{
			uuid: "invalid",
			err:  coreerrors.NotValid,
		},
		{
			uuid: uuid.MustNewUUID().String(),
		},
	}

	for i, test := range tests {
		c.Logf("test %d: %q", i, test.uuid)
		err := UnitUUID(test.uuid).Validate()

		if test.err == nil {
			c.Check(err, tc.IsNil)
			continue
		}

		c.Check(err, tc.ErrorIs, test.err)
	}
}

type relationEndpointUUIDSuite struct {
	testhelpers.IsolationSuite
}

var _ = tc.Suite(&relationEndpointUUIDSuite{})

func (*relationEndpointUUIDSuite) TestUUIDValidate(c *tc.C) {
	// Test that the uuid.Validate method succeeds and
	// fails as expected.
	tests := []struct {
		uuid string
		err  error
	}{
		{
			uuid: "",
			err:  coreerrors.NotValid,
		},
		{
			uuid: "invalid",
			err:  coreerrors.NotValid,
		},
		{
			uuid: uuid.MustNewUUID().String(),
		},
	}

	for i, test := range tests {
		c.Logf("test %d: %q", i, test.uuid)
		err := EndpointUUID(test.uuid).Validate()

		if test.err == nil {
			c.Check(err, tc.IsNil)
			continue
		}

		c.Check(err, tc.ErrorIs, test.err)
	}
}
