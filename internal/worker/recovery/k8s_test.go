// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/core/application"
	"github.com/juju/juju/core/machine"
	"github.com/juju/juju/core/unit"
	"github.com/juju/juju/internal/errors"
)

type k8sSuite struct{}

func TestK8sSuite(t *testing.T) { tc.Run(t, &k8sSuite{}) }

type recoveredPasswords struct {
	applicationUUID                                        application.UUID
	applicationPassword, unitPassword, nodePassword, nonce string
	unitName                                               unit.Name
	controllerID                                           string
	failUnit                                               bool
}

func (p *recoveredPasswords) GetApplicationUUIDByName(context.Context, string) (application.UUID, error) {
	return application.UUID("source-application"), nil
}
func (p *recoveredPasswords) SetApplicationPassword(_ context.Context, id application.UUID, password string) error {
	p.applicationUUID, p.applicationPassword = id, password
	return nil
}
func (p *recoveredPasswords) SetUnitPassword(_ context.Context, name unit.Name, password string) error {
	if p.failUnit {
		return errors.New("unit password failure")
	}
	p.unitName, p.unitPassword = name, password
	return nil
}
func (p *recoveredPasswords) SetMachinePassword(context.Context, machine.Name, string) error {
	return errors.New("unexpected machine password mutation")
}
func (p *recoveredPasswords) SetControllerNodePassword(_ context.Context, id, password string) error {
	p.controllerID, p.nodePassword = id, password
	return nil
}
func (p *recoveredPasswords) EnsureControllerNodeNonce(_ context.Context, id, nonce string) (string, error) {
	if p.nonce == "" {
		p.nonce = nonce
	}
	return p.nonce, nil
}

func (s *k8sSuite) TestPasswordFinalisationRetry(c *tc.C) {
	cfg := k8sPasswordConfig{ControllerID: "0", AgentPassword: "target-node", ApplicationPassword: "target-app", UnitPassword: "target-unit", NoncePath: filepath.Join(c.MkDir(), "nonce")}
	c.Assert(os.WriteFile(cfg.NoncePath, []byte("target-nonce"), 0600), tc.ErrorIsNil)
	passwords := &recoveredPasswords{failUnit: true}
	c.Check(finaliseK8sPasswords(c.Context(), passwords, passwords, cfg), tc.ErrorMatches, "unit password failure")
	c.Check(passwords.applicationPassword, tc.Equals, "target-app")
	c.Check(passwords.nodePassword, tc.Equals, "")
	passwords.failUnit = false
	for range 2 {
		c.Assert(finaliseK8sPasswords(c.Context(), passwords, passwords, cfg), tc.ErrorIsNil)
	}
	c.Check(passwords.applicationUUID, tc.Equals, application.UUID("source-application"))
	c.Check(passwords.unitName, tc.Equals, unit.Name("controller/0"))
	c.Check(passwords.unitPassword, tc.Equals, "target-unit")
	c.Check(passwords.controllerID, tc.Equals, "0")
	c.Check(passwords.nodePassword, tc.Equals, "target-node")
	c.Check(passwords.nonce, tc.Equals, "target-nonce")
}

func (s *k8sSuite) TestMissingNonceFailsBeforePasswordWrites(c *tc.C) {
	cfg := k8sPasswordConfig{ControllerID: "0", AgentPassword: "target-node", ApplicationPassword: "target-app", UnitPassword: "target-unit", NoncePath: filepath.Join(c.MkDir(), "absent")}
	passwords := &recoveredPasswords{}
	err := finaliseK8sPasswords(c.Context(), passwords, passwords, cfg)
	c.Check(err, tc.ErrorIs, os.ErrNotExist)
	c.Check(passwords.applicationPassword, tc.Equals, "")
}

func (s *k8sSuite) TestConflictingNonceFailsClosed(c *tc.C) {
	cfg := k8sPasswordConfig{ControllerID: "0", AgentPassword: "target-node", ApplicationPassword: "target-app", UnitPassword: "target-unit", NoncePath: filepath.Join(c.MkDir(), "nonce")}
	c.Assert(os.WriteFile(cfg.NoncePath, []byte("target-nonce"), 0600), tc.ErrorIsNil)
	passwords := &recoveredPasswords{nonce: "previous-nonce"}
	c.Check(finaliseK8sPasswords(c.Context(), passwords, passwords, cfg), tc.ErrorMatches, "replacement introduction nonce disagrees with persisted nonce")
	c.Check(passwords.nonce, tc.Equals, "previous-nonce")
}
