// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"os"
	"path/filepath"
	stdtesting "testing"

	"github.com/juju/tc"

	"github.com/juju/juju/internal/recovery"
	"github.com/juju/juju/internal/testhelpers"
)

type paramsSuite struct {
	testhelpers.IsolationSuite
}

func TestParamsSuite(t *stdtesting.T) {
	tc.Run(t, &paramsSuite{})
}

func (s *paramsSuite) TestIsRecoveryMissing(c *tc.C) {
	isRecovery, err := recovery.IsRecovery(c.MkDir())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(isRecovery, tc.IsFalse)
}

func (s *paramsSuite) TestIsRecoveryEmptyFile(c *tc.C) {
	dataDir := c.MkDir()
	writeParams(c, dataDir, "")
	isRecovery, err := recovery.IsRecovery(dataDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(isRecovery, tc.IsTrue)
}

func (s *paramsSuite) TestIsRecoveryPopulatedFile(c *tc.C) {
	dataDir := c.MkDir()
	writeParams(c, dataDir, "archive-path: /var/lib/juju/recovery/archive.tar.gz\n")
	isRecovery, err := recovery.IsRecovery(dataDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(isRecovery, tc.IsTrue)
}

func (s *paramsSuite) TestIsRecoveryDirectory(c *tc.C) {
	dataDir := c.MkDir()
	paramsPath := filepath.Join(dataDir, recovery.ParamsFile)
	c.Assert(os.MkdirAll(paramsPath, 0o700), tc.ErrorIsNil)
	isRecovery, err := recovery.IsRecovery(dataDir)
	c.Check(isRecovery, tc.IsFalse)
	c.Check(err, tc.ErrorMatches,
		`invalid recovery params path ".*/recovery/params": expected regular file`)
}

func writeParams(c *tc.C, dataDir, contents string) {
	paramsPath := filepath.Join(dataDir, recovery.ParamsFile)
	c.Assert(os.MkdirAll(filepath.Dir(paramsPath), 0o700), tc.ErrorIsNil)
	c.Assert(os.WriteFile(paramsPath, []byte(contents), 0o600), tc.ErrorIsNil)
}
