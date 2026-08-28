// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller

import (
	"context"
	"database/sql"
	"testing"

	"github.com/canonical/sqlair"
	"github.com/juju/tc"

	"github.com/juju/juju/domain/export/types/controller/v4_1_0"
	schematesting "github.com/juju/juju/domain/schema/testing"
	"github.com/juju/juju/internal/errors"
)

type overlaySuiteV4_1_0 struct {
	schematesting.ControllerModelSuite
}

func TestOverlaySuiteV4_1_0(t *testing.T) {
	tc.Run(t, &overlaySuiteV4_1_0{})
}

type controllerRow struct {
	UUID      string `db:"uuid"`
	ModelUUID string `db:"model_uuid"`
}

// TestApplyOverlayRestoresControllerIdentity asserts the overlay updates the
// singleton controller row to the source identity (uuid + controller-model
// uuid), preserving the target-local api_port/cert/keys.
func (s *overlaySuiteV4_1_0) TestApplyOverlayRestoresControllerIdentity(c *tc.C) {
	sourceUUID := "source-controller-uuid"

	ctrlDB := s.ControllerTxnRunner()
	targetUUID := s.SeedControllerTable(c, "target-controller-model")

	payload := &v4_1_0.ControllerExport{}
	overlay := ControllerOverlay{
		ControllerID:         "0",
		TargetControllerUUID: targetUUID,
		SourceController:     v4_1_0.Controller{UUID: sourceUUID, ModelUUID: "source-controller-model"},
	}

	st := NewState(s.TxnRunnerFactory())
	err := st.ApplyOverlay(c.Context(), payload, overlay)
	c.Assert(err, tc.ErrorIsNil)

	stmt, err := sqlair.Prepare(`SELECT &controllerRow.* FROM "controller"`, controllerRow{})
	c.Assert(err, tc.ErrorIsNil)
	var row controllerRow
	err = ctrlDB.Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmt).Get(&row))
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(row.UUID, tc.Equals, sourceUUID)
	c.Check(row.ModelUUID, tc.Equals, "source-controller-model")
}

// TestApplyOverlayNormalisesObjectStorePlacement asserts restored placement
// rows are rewritten to node 0.
func (s *overlaySuiteV4_1_0) TestApplyOverlayNormalisesObjectStorePlacement(c *tc.C) {
	ctrlDB := s.ControllerTxnRunner()
	stmtMeta, err := sqlair.Prepare(`INSERT INTO "object_store_metadata" (uuid, sha_256, sha_384, size) VALUES ('blob', 'sha256', 'sha384', 1)`)
	c.Assert(err, tc.ErrorIsNil)
	err = ctrlDB.Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmtMeta).Run())
	})
	c.Assert(err, tc.ErrorIsNil)

	payload := &v4_1_0.ControllerExport{
		ObjectStorePlacement: []v4_1_0.ObjectStorePlacement{{UUID: "blob", NodeID: "9"}},
	}
	overlay := ControllerOverlay{
		ControllerID:     "0",
		SourceController: v4_1_0.Controller{UUID: "source-controller-uuid", ModelUUID: "source-controller-model"},
	}

	st := NewState(s.TxnRunnerFactory())
	err = st.ApplyOverlay(c.Context(), payload, overlay)
	c.Assert(err, tc.ErrorIsNil)

	ctrlDB = s.ControllerTxnRunner()
	stmt, err := sqlair.Prepare(`SELECT &ObjectStorePlacement.* FROM "object_store_placement"`, v4_1_0.ObjectStorePlacement{})
	c.Assert(err, tc.ErrorIsNil)
	var rows []v4_1_0.ObjectStorePlacement
	err = ctrlDB.Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmt).GetAll(&rows))
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rows, tc.HasLen, 1)
	c.Check(rows[0].NodeID, tc.Equals, "0")
}

// TestApplyOverlayMakesNamespaceListAuthoritative asserts target-only
// namespaces are removed and source namespaces added.
func (s *overlaySuiteV4_1_0) TestApplyOverlayMakesNamespaceListAuthoritative(c *tc.C) {
	payload := &v4_1_0.ControllerExport{}
	overlay := ControllerOverlay{
		ControllerID:         "0",
		TargetControllerUUID: "target-controller-uuid",
		SourceController:     v4_1_0.Controller{UUID: "source-controller-uuid", ModelUUID: "source-controller-model"},
		RemoveNamespaces:     []v4_1_0.NamespaceList{{Namespace: "target-only-ns"}},
		AddNamespaces:        []v4_1_0.NamespaceList{{Namespace: "source-model-ns"}},
	}

	ctrlDB := s.ControllerTxnRunner()
	stmtIns, err := sqlair.Prepare(`INSERT INTO "namespace_list" (namespace) VALUES ($NamespaceList.namespace)`, v4_1_0.NamespaceList{})
	c.Assert(err, tc.ErrorIsNil)
	err = ctrlDB.Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmtIns, v4_1_0.NamespaceList{Namespace: "target-only-ns"}).Run())
	})
	c.Assert(err, tc.ErrorIsNil)

	st := NewState(s.TxnRunnerFactory())
	err = st.ApplyOverlay(c.Context(), payload, overlay)
	c.Assert(err, tc.ErrorIsNil)

	stmt, err := sqlair.Prepare(`SELECT &NamespaceList.* FROM "namespace_list"`, v4_1_0.NamespaceList{})
	c.Assert(err, tc.ErrorIsNil)
	var rows []v4_1_0.NamespaceList
	err = ctrlDB.Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmt).GetAll(&rows))
	})
	c.Assert(err, tc.ErrorIsNil)
	namespaces := map[string]bool{}
	for _, r := range rows {
		namespaces[r.Namespace] = true
	}
	c.Check(namespaces["source-model-ns"], tc.IsTrue)
	c.Check(namespaces["target-only-ns"], tc.IsFalse)
}

// TestDeleteModel asserts DeleteModel removes the temporary controller-model
// row together with its dependent rows so the source controller model can be
// imported on the same qualified name.
func (s *overlaySuiteV4_1_0) TestDeleteModel(c *tc.C) {
	s.SeedControllerTable(c, "target-controller-model")

	// Seed a cloud, the temporary controller-model row, and a dependent
	// model_namespace row.
	err := s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO "cloud" (uuid, name, cloud_type_id, endpoint, skip_tls_verify) VALUES ('cloud-1', 'dummy', 1, '', 0)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO "model" (uuid, activated, cloud_uuid, model_type_id, life_id, name, qualifier) VALUES ('temp-ctrl-model', 1, 'cloud-1', 0, 0, 'controller', 'admin')`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO "model_namespace" (namespace, model_uuid) VALUES ('temp-ctrl-model', 'temp-ctrl-model')`); err != nil {
			return err
		}
		return nil
	})
	c.Assert(err, tc.ErrorIsNil)

	st := NewState(s.TxnRunnerFactory())
	err = st.DeleteModel(c.Context(), "temp-ctrl-model")
	c.Assert(err, tc.ErrorIsNil)

	// The temporary model row and its dependents are gone.
	err = s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM "model" WHERE uuid = 'temp-ctrl-model'`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return errors.New("model row not deleted")
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM "model_namespace" WHERE model_uuid = 'temp-ctrl-model'`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return errors.New("model_namespace row not deleted")
		}
		return nil
	})
	c.Assert(err, tc.ErrorIsNil)
}

// TestApplyOverlayNamespaceInsertIsIdempotent asserts that adding a namespace
// already registered by the pre-import registration step succeeds, matching
// the register-then-overlay sequence the restore orchestration runs.
func (s *overlaySuiteV4_1_0) TestApplyOverlayNamespaceInsertIsIdempotent(c *tc.C) {
	payload := &v4_1_0.ControllerExport{}
	overlay := ControllerOverlay{
		ControllerID:     "0",
		SourceController: v4_1_0.Controller{UUID: "source-controller-uuid", ModelUUID: "source-controller-model"},
		AddNamespaces:    []v4_1_0.NamespaceList{{Namespace: "already-registered"}},
	}

	ctrlDB := s.ControllerTxnRunner()
	stmtIns, err := sqlair.Prepare(`INSERT INTO "namespace_list" (namespace) VALUES ($NamespaceList.namespace)`, v4_1_0.NamespaceList{})
	c.Assert(err, tc.ErrorIsNil)
	err = ctrlDB.Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmtIns, v4_1_0.NamespaceList{Namespace: "already-registered"}).Run())
	})
	c.Assert(err, tc.ErrorIsNil)

	st := NewState(s.TxnRunnerFactory())
	err = st.ApplyOverlay(c.Context(), payload, overlay)
	c.Assert(err, tc.ErrorIsNil)

	stmt, err := sqlair.Prepare(`SELECT &NamespaceList.* FROM "namespace_list"`, v4_1_0.NamespaceList{})
	c.Assert(err, tc.ErrorIsNil)
	var rows []v4_1_0.NamespaceList
	err = ctrlDB.Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		return errors.Capture(tx.Query(ctx, stmt).GetAll(&rows))
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(len(rows), tc.Equals, 1)
	c.Check(rows[0].Namespace, tc.Equals, "already-registered")
}
