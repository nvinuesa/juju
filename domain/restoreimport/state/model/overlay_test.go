// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package model

import (
	"context"
	"testing"

	"github.com/canonical/sqlair"
	"github.com/juju/tc"

	"github.com/juju/juju/domain/export/types/v4_1_0"
	schematesting "github.com/juju/juju/domain/schema/testing"
	"github.com/juju/juju/internal/errors"
)

type overlaySuiteV4_1_0 struct {
	schematesting.ModelSuite
}

func TestOverlaySuiteV4_1_0(t *testing.T) {
	tc.Run(t, &overlaySuiteV4_1_0{})
}

func (s *overlaySuiteV4_1_0) seedModel(c *tc.C) {
	err := s.TxnRunner().Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		stmt, err := sqlair.Prepare(`INSERT INTO "model" (uuid, controller_uuid, name, qualifier, type, cloud, cloud_type) VALUES ($Model.uuid, 'ctrl', 'test', 'admin', 'iaas', 'dummy', 'lxd')`, v4_1_0.Model{})
		if err != nil {
			return err
		}
		return errors.Capture(tx.Query(ctx, stmt, v4_1_0.Model{UUID: s.ModelUUID()}).Run())
	})
	c.Assert(err, tc.ErrorIsNil)
}

func (s *overlaySuiteV4_1_0) readModelAgent(c *tc.C) v4_1_0.ModelAgent {
	stmt, err := sqlair.Prepare(`SELECT &ModelAgent.* FROM "model_agent"`, v4_1_0.ModelAgent{})
	c.Assert(err, tc.ErrorIsNil)
	var row v4_1_0.ModelAgent
	err = s.TxnRunner().Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmt).Get(&row))
	})
	c.Assert(err, tc.ErrorIsNil)
	return row
}

// TestApplyOverlayMergesModelAgentPassword asserts the restored model_agent
// row carries the target-local password hash after the overlay.
func (s *overlaySuiteV4_1_0) TestApplyOverlayMergesModelAgentPassword(c *tc.C) {
	s.seedModel(c)

	err := s.TxnRunner().Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		stmt, err := sqlair.Prepare(`INSERT INTO "model_agent" (model_uuid, password_hash_algorithm_id, password_hash) VALUES ($ModelAgent.model_uuid, $ModelAgent.password_hash_algorithm_id, $ModelAgent.password_hash)`, v4_1_0.ModelAgent{})
		if err != nil {
			return err
		}
		return errors.Capture(tx.Query(ctx, stmt, v4_1_0.ModelAgent{
			ModelUUID:    s.ModelUUID(),
			PasswordHash: new("source-hash"),
		}).Run())
	})
	c.Assert(err, tc.ErrorIsNil)

	payload := &v4_1_0.ModelExport{
		ModelAgent: []v4_1_0.ModelAgent{{ModelUUID: s.ModelUUID(), PasswordHash: new("source-hash")}},
	}
	st := NewState(s.TxnRunnerFactory())
	err = st.ApplyOverlay(c.Context(), payload, ModelOverlay{NodeID: "0", ModelAgentPasswordHash: "target-hash"})
	c.Assert(err, tc.ErrorIsNil)

	row := s.readModelAgent(c)
	c.Assert(row.PasswordHash, tc.NotNil)
	c.Check(*row.PasswordHash, tc.Equals, "target-hash")
}

// TestApplyOverlaySkipsEmptyPasswordHash asserts the overlay does not touch
// the restored model_agent row when no target hash was captured (e.g.
// non-controller models).
func (s *overlaySuiteV4_1_0) TestApplyOverlaySkipsEmptyPasswordHash(c *tc.C) {
	s.seedModel(c)

	err := s.TxnRunner().Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		stmt, err := sqlair.Prepare(`INSERT INTO "model_agent" (model_uuid, password_hash) VALUES ($ModelAgent.model_uuid, $ModelAgent.password_hash)`, v4_1_0.ModelAgent{})
		if err != nil {
			return err
		}
		return errors.Capture(tx.Query(ctx, stmt, v4_1_0.ModelAgent{
			ModelUUID:    s.ModelUUID(),
			PasswordHash: new("source-hash"),
		}).Run())
	})
	c.Assert(err, tc.ErrorIsNil)

	payload := &v4_1_0.ModelExport{
		ModelAgent: []v4_1_0.ModelAgent{{ModelUUID: s.ModelUUID(), PasswordHash: new("source-hash")}},
	}
	st := NewState(s.TxnRunnerFactory())
	err = st.ApplyOverlay(c.Context(), payload, ModelOverlay{NodeID: "0"})
	c.Assert(err, tc.ErrorIsNil)

	row := s.readModelAgent(c)
	c.Assert(row.PasswordHash, tc.NotNil)
	c.Check(*row.PasswordHash, tc.Equals, "source-hash")
}

// TestApplyOverlayRewritesPlacement asserts restored placement rows are
// rewritten to the restore node.
func (s *overlaySuiteV4_1_0) TestApplyOverlayRewritesPlacement(c *tc.C) {
	err := s.TxnRunner().Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		stmt, err := sqlair.Prepare(`INSERT INTO "object_store_metadata" (uuid, sha_256, sha_384, size) VALUES ('blob', 'sha256', 'sha384', 1)`)
		if err != nil {
			return err
		}
		return errors.Capture(tx.Query(ctx, stmt).Run())
	})
	c.Assert(err, tc.ErrorIsNil)

	payload := &v4_1_0.ModelExport{
		ObjectStorePlacement: []v4_1_0.ObjectStorePlacement{{UUID: "blob", NodeID: "9"}},
	}
	st := NewState(s.TxnRunnerFactory())
	err = st.ApplyOverlay(c.Context(), payload, ModelOverlay{NodeID: "0"})
	c.Assert(err, tc.ErrorIsNil)

	stmt, err := sqlair.Prepare(`SELECT &ObjectStorePlacement.* FROM "object_store_placement"`, v4_1_0.ObjectStorePlacement{})
	c.Assert(err, tc.ErrorIsNil)
	var rows []v4_1_0.ObjectStorePlacement
	err = s.TxnRunner().Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmt).GetAll(&rows))
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rows, tc.HasLen, 1)
	c.Check(rows[0].NodeID, tc.Equals, "0")
}
