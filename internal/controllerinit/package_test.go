// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controllerinit

import (
	"github.com/canonical/gomock/gomock"
	"github.com/juju/tc"

	"github.com/juju/juju/internal/testhelpers"
)

//go:generate go run github.com/canonical/gomock/mockgen -package controllerinit -destination agentpasswordservice_mock_test.go github.com/juju/juju/internal/controllerinit AgentPasswordService

type baseSuite struct {
	testhelpers.IsolationSuite

	agentPasswordService *MockAgentPasswordService
}

func (s *baseSuite) setupMocks(c *tc.C) *gomock.Controller {
	ctrl := gomock.NewController(c)
	s.agentPasswordService = NewMockAgentPasswordService(ctrl)
	return ctrl
}
