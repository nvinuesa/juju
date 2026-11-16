// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state_test

import (
	"database/sql"
	stdtesting "testing"

	"github.com/juju/tc"

	"github.com/juju/juju/domain/recovery/state"
	schematesting "github.com/juju/juju/domain/schema/testing"
)

type patchSuite struct{ schematesting.ControllerSuite }

func TestPatchSuite(t *stdtesting.T) {
	tc.Run(t, &patchSuite{})
}

const patchModelUUID = "c0ffee00-aaaa-2222-3333-444455556666"

// TestPatchControllerCredentialSkipsMissingCredential checks that a model
// with no recorded credential, as produced by a credential-less local
// controller, is left untouched rather than failing recovery.
func (s *patchSuite) TestPatchControllerCredentialSkipsMissingCredential(c *tc.C) {
	seedCloudAndUser(c, s.DB())
	execSQL(c, s.DB(), `
INSERT INTO model (uuid, name, qualifier, model_type_id, life_id, cloud_uuid)
VALUES (?, 'test', 'prod', 0, 0, 'cloud')`, patchModelUUID)

	err := state.NewState(s.TxnRunnerFactory()).PatchControllerCredential(
		c.Context(), patchModelUUID, "access-key", map[string]string{"key": "value"})
	c.Assert(err, tc.ErrorIsNil)
}

// TestPatchControllerCredentialRebindsAuthentication checks that the archived
// credential identity is retained while its auth type and attributes take the
// replacement's values.
func (s *patchSuite) TestPatchControllerCredentialRebindsAuthentication(c *tc.C) {
	seedCloudAndUser(c, s.DB())
	execSQL(c, s.DB(), `
INSERT INTO cloud_credential (uuid, cloud_uuid, auth_type_id, owner_uuid, name, revoked, invalid)
VALUES ('cred', 'cloud', 1, 'owner', 'cred', true, true)`)
	execSQL(c, s.DB(), `
INSERT INTO cloud_credential_attribute (cloud_credential_uuid, key, value)
VALUES ('cred', 'old', 'archived')`)
	execSQL(c, s.DB(), `
INSERT INTO model (uuid, name, qualifier, model_type_id, life_id, cloud_uuid, cloud_credential_uuid)
VALUES (?, 'test', 'prod', 0, 0, 'cloud', 'cred')`, patchModelUUID)

	err := state.NewState(s.TxnRunnerFactory()).PatchControllerCredential(
		c.Context(), patchModelUUID, "access-key", map[string]string{"key": "value"})
	c.Assert(err, tc.ErrorIsNil)

	checkString(c, s.DB(), `SELECT auth_type_id FROM cloud_credential WHERE uuid = 'cred'`, "0")
	checkString(c, s.DB(), `SELECT CAST(revoked AS TEXT) FROM cloud_credential WHERE uuid = 'cred'`, "0")
	checkString(c, s.DB(), `SELECT CAST(invalid AS TEXT) FROM cloud_credential WHERE uuid = 'cred'`, "0")
	var key, value string
	err = s.DB().QueryRowContext(c.Context(), `
SELECT key, value FROM cloud_credential_attribute WHERE cloud_credential_uuid = 'cred'`).
		Scan(&key, &value)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(key, tc.Equals, "key")
	c.Check(value, tc.Equals, "value")
}

// TestGetControllerMachineNameRequiresLiveController checks that recovery
// fails closed rather than assuming machine 0 when the archive has no live
// controller machine.
func (s *modelSuite) TestGetControllerMachineNameRequiresLiveController(c *tc.C) {
	_, err := state.NewState(s.TxnRunnerFactory()).GetControllerMachineName(c.Context())
	c.Check(err, tc.ErrorMatches, "archive contains no live controller machine")
}

func seedCloudAndUser(c *tc.C, db *sql.DB) {
	execSQL(c, db, `
INSERT INTO user (uuid, name, display_name, external, removed, created_by_uuid, created_at)
VALUES ('owner', 'owner', 'Owner', false, false, 'owner', CURRENT_TIMESTAMP)`)
	execSQL(c, db, `
INSERT INTO cloud (uuid, name, cloud_type_id, endpoint, skip_tls_verify)
VALUES ('cloud', 'test-cloud', 1, '', false)`)
}
