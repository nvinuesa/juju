// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agent

import (
	"context"
	"os"
	"path/filepath"
	stdtesting "testing"

	"github.com/juju/tc"

	"github.com/juju/juju/core/network"
	"github.com/juju/juju/environs"
)

type BootstrapSuite struct {
}

func TestBootstrapSuite(t *stdtesting.T) {
	tc.Run(t, &BootstrapSuite{})
}

func (s *BootstrapSuite) TestStub(c *tc.C) {
	c.Skipf(`This suite is missing tests for the following scenarios:

  - Test that the initial model and all of the machines are created.
  - Test JWKS reachable check.
  - Test that the initial model is created with the correct UUID.
  - Test initial password.
  - Test set constraints for bootstrap machine and model.
  - Test system identity is written to the data directory.
`)
}

func (s *BootstrapSuite) TestBootstrapControllerAddressesUsesProviderInterface(c *tc.C) {
	want := network.NewMachineAddresses([]string{"10.0.0.1"}).AsProviderAddresses()
	env := &stubBootstrapAddressEnviron{addresses: want}

	got, err := bootstrapControllerAddresses(c.Context(), env, "bootstrap-instance")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(got, tc.DeepEquals, want)
	c.Check(env.calls, tc.Equals, 1)
}

func (s *BootstrapSuite) TestWriteRecoveryCompleteMarker(c *tc.C) {
	dataDir := c.MkDir()
	err := writeRecoveryCompleteMarker(dataDir, "abc123")
	c.Assert(err, tc.ErrorIsNil)

	markerPath := filepath.Join(dataDir, "recovery", "bootstrap-complete")
	data, err := os.ReadFile(markerPath)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(data), tc.Equals, "abc123\n")

	info, err := os.Stat(markerPath)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(info.Mode().Perm(), tc.Equals, os.FileMode(0o600))

	dirInfo, err := os.Stat(filepath.Dir(markerPath))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(dirInfo.Mode().Perm(), tc.Equals, os.FileMode(0o700))
}

func (s *BootstrapSuite) TestWriteRecoveryCompleteMarkerOverwrites(c *tc.C) {
	dataDir := c.MkDir()
	c.Assert(writeRecoveryCompleteMarker(dataDir, "abc123"), tc.ErrorIsNil)
	c.Assert(writeRecoveryCompleteMarker(dataDir, "def456"), tc.ErrorIsNil)

	data, err := os.ReadFile(filepath.Join(dataDir, "recovery", "bootstrap-complete"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(data), tc.Equals, "def456\n")
}

type stubBootstrapAddressEnviron struct {
	environs.BootstrapEnviron
	addresses network.ProviderAddresses
	calls     int
}

func (e *stubBootstrapAddressEnviron) BootstrapControllerAddresses(
	_ context.Context,
) (network.ProviderAddresses, error) {
	e.calls++
	return e.addresses, nil
}
