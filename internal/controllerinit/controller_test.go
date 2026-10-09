// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controllerinit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/canonical/gomock/gomock"
	"github.com/juju/tc"

	"github.com/juju/juju/internal/errors"
)

type controllerSuite struct {
	baseSuite
}

func TestControllerSuite(t *testing.T) {
	tc.Run(t, &controllerSuite{})
}

func (s *controllerSuite) TestFinaliseK8sAgentUsesControllerIdentity(c *tc.C) {
	defer s.setupMocks(c).Finish()
	noncePath := filepath.Join(c.MkDir(), "nonce")
	c.Assert(os.WriteFile(noncePath, []byte("selected-nonce"), 0600), tc.ErrorIsNil)
	gomock.InOrder(
		s.agentPasswordService.EXPECT().SetControllerNodePassword(gomock.Any(), "7", "agent-password").Return(nil),
		s.agentPasswordService.EXPECT().EnsureControllerNodeNonce(gomock.Any(), "7", "selected-nonce").Return("persisted-nonce", nil),
	)
	err := FinaliseK8sAgent(c.Context(), s.agentPasswordService, "7", "agent-password", noncePath)
	c.Assert(err, tc.ErrorIsNil)
}

func (s *controllerSuite) TestFinaliseK8sAgentWithoutNonceFile(c *tc.C) {
	defer s.setupMocks(c).Finish()
	s.agentPasswordService.EXPECT().SetControllerNodePassword(gomock.Any(), "7", "agent-password").Return(nil)
	err := FinaliseK8sAgent(c.Context(), s.agentPasswordService, "7", "agent-password", filepath.Join(c.MkDir(), "absent"))
	c.Assert(err, tc.ErrorIsNil)
}

func (s *controllerSuite) TestFinaliseK8sAgentNonceReadFailure(c *tc.C) {
	defer s.setupMocks(c).Finish()
	// A directory as the nonce path fails to read with an error other than
	// non-existence. Finalisation must fail rather than skip the nonce, so
	// EnsureControllerNodeNonce is never called.
	s.agentPasswordService.EXPECT().SetControllerNodePassword(gomock.Any(), "7", "agent-password").Return(nil)
	err := FinaliseK8sAgent(c.Context(), s.agentPasswordService, "7", "agent-password", c.MkDir())
	c.Check(err, tc.NotNil)
}

func (s *controllerSuite) TestFinaliseK8sAgentNonceFailure(c *tc.C) {
	defer s.setupMocks(c).Finish()
	expected := errors.New("nonce failed")
	noncePath := filepath.Join(c.MkDir(), "nonce")
	c.Assert(os.WriteFile(noncePath, []byte("selected-nonce"), 0600), tc.ErrorIsNil)
	s.agentPasswordService.EXPECT().SetControllerNodePassword(gomock.Any(), "7", "agent-password").Return(nil)
	s.agentPasswordService.EXPECT().EnsureControllerNodeNonce(gomock.Any(), "7", "selected-nonce").Return("", expected)
	err := FinaliseK8sAgent(c.Context(), s.agentPasswordService, "7", "agent-password", noncePath)
	c.Check(err, tc.ErrorIs, expected)
}

func (s *controllerSuite) TestFinaliseK8sAgentPasswordBeforeNonceRead(c *tc.C) {
	defer s.setupMocks(c).Finish()
	noncePath := filepath.Join(c.MkDir(), "nonce")
	s.agentPasswordService.EXPECT().SetControllerNodePassword(gomock.Any(), "7", "agent-password").
		DoAndReturn(func(context.Context, string, string) error {
			return os.WriteFile(noncePath, []byte("selected-nonce"), 0600)
		})
	s.agentPasswordService.EXPECT().EnsureControllerNodeNonce(gomock.Any(), "7", "selected-nonce").Return("selected-nonce", nil)
	err := FinaliseK8sAgent(c.Context(), s.agentPasswordService, "7", "agent-password", noncePath)
	c.Assert(err, tc.ErrorIsNil)
}
