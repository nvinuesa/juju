// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/canonical/sqlair"
	"github.com/juju/names/v6"
	"github.com/juju/tc"
	"gopkg.in/yaml.v3"

	"github.com/juju/juju/agent"
	corebackups "github.com/juju/juju/core/backups"
	"github.com/juju/juju/core/database"
	"github.com/juju/juju/core/model"
	domainexport "github.com/juju/juju/domain/export"
	ctrlv4_1_0 "github.com/juju/juju/domain/export/types/controller/v4_1_0"
	modelv4_1_0 "github.com/juju/juju/domain/export/types/v4_1_0"
	schematesting "github.com/juju/juju/domain/schema/testing"
	"github.com/juju/juju/internal/errors"
	internallogger "github.com/juju/juju/internal/logger"
	"github.com/juju/juju/internal/uuid"
)

var (
	srcCtrlUUID      = uuid.MustNewUUID().String()
	srcCtrlModelUUID = uuid.MustNewUUID().String()
	tempCtrlUUID     = uuid.MustNewUUID().String()
	tempCtrlModelNS  = uuid.MustNewUUID().String()
)

// fakeDBGetter serves the controller DB plus per-model schema DBs, mimicking
// the reduced-runtime db-accessor: it refuses unregistered namespaces.
type fakeDBGetter struct {
	c         *tc.C
	suite     *schematesting.ControllerModelSuite
	opened    map[string]database.TxnRunner
	registred map[string]bool
}

func (f *fakeDBGetter) GetDB(ctx context.Context, ns string) (database.TxnRunner, error) {
	if ns == database.ControllerNS {
		return f.suite.ControllerTxnRunner(), nil
	}
	// Like the real db-accessor, refuse unregistered namespaces: consult the
	// controller DB's namespace_list.
	var n int
	err := f.suite.ControllerTxnRunner().StdTxn(ctx, func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM namespace_list WHERE namespace = ?`, ns).Scan(&n)
	})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, errors.New("namespace not registered")
	}
	if r, ok := f.opened[ns]; ok {
		return r, nil
	}
	r := f.suite.ModelTxnRunner(f.c, ns)
	f.opened[ns] = r
	return r, nil
}

type fakeDBDeleter struct {
	deleted []string
}

func (f *fakeDBDeleter) DeleteDB(ns string) error {
	f.deleted = append(f.deleted, ns)
	return nil
}

type e2eSuite struct {
	schematesting.ControllerModelSuite
}

func TestE2ESuite(t *testing.T) {
	tc.Run(t, &e2eSuite{})
}

func (s *e2eSuite) SetUpTest(c *tc.C) {
	s.ControllerModelSuite.SetUpTest(c)

	// Seed the target like a fresh bootstrap: temp controller row, node 0,
	// a single cloud/credential, the temporary controller model, and its
	// namespace registrations.
	err := s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO controller (uuid, model_uuid, target_version, api_port) VALUES (?, ?, '4.1.0', '17070')`, tempCtrlUUID, tempCtrlModelNS); err != nil {
			return err
		}
		// controller_node for node 0 is pre-seeded by the suite.
		if _, err := tx.ExecContext(ctx, `INSERT INTO cloud (uuid, name, cloud_type_id, endpoint, skip_tls_verify) VALUES ('temp-cloud', 'dummy', 1, '', 0)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO cloud_region (uuid, cloud_uuid, name) VALUES ('temp-region', 'temp-cloud', 'localhost')`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO "user" (uuid, name, external, created_by_uuid, created_at) VALUES ('user-1', 'admin', 0, 'user-1', CURRENT_TIMESTAMP)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO cloud_credential (uuid, cloud_uuid, auth_type_id, owner_uuid, name, revoked) VALUES ('temp-cred', 'temp-cloud', '2', 'user-1', 'default', 0)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO model (uuid, activated, cloud_uuid, cloud_region_uuid, cloud_credential_uuid, model_type_id, life_id, name, qualifier) VALUES (?, 1, 'temp-cloud', 'temp-region', 'temp-cred', 0, 0, 'controller', 'admin')`, tempCtrlModelNS); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO model_namespace (namespace, model_uuid) VALUES (?, ?)`, tempCtrlModelNS, tempCtrlModelNS); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO namespace_list (namespace) VALUES ('controller'), (?)`, tempCtrlModelNS); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO lease (uuid, lease_type_id, model_uuid, name, holder, start, expiry) VALUES ('lease-1', 0, '', 'singular-controller', '0', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
			return err
		}
		return nil
	})
	c.Assert(err, tc.ErrorIsNil)

	// Seed the temporary controller model DB with a model row and a
	// target-local agent password.
	tempModelDB := s.ModelTxnRunner(c, tempCtrlModelNS)
	err = tempModelDB.Txn(c.Context(), func(ctx context.Context, tx *sqlair.TX) error {
		modelStmt, err := sqlair.Prepare(`INSERT INTO "model" (uuid, controller_uuid, name, qualifier, type, cloud, cloud_type, is_controller_model) VALUES ($Model.uuid, $Model.controller_uuid, 'controller', 'admin', 'iaas', 'dummy', 'lxd', 1)`, modelv4_1_0.Model{})
		if err != nil {
			return err
		}
		if err := tx.Query(ctx, modelStmt, modelv4_1_0.Model{UUID: tempCtrlModelNS, ControllerUUID: tempCtrlUUID}).Run(); err != nil {
			return err
		}
		agentStmt, err := sqlair.Prepare(`INSERT INTO "model_agent" (model_uuid, password_hash) VALUES ($ModelAgent.model_uuid, $ModelAgent.password_hash)`, modelv4_1_0.ModelAgent{})
		if err != nil {
			return err
		}
		if err := tx.Query(ctx, agentStmt, modelv4_1_0.ModelAgent{ModelUUID: tempCtrlModelNS, PasswordHash: new("target-hash")}).Run(); err != nil {
			return err
		}
		// machine-0 with a target-local password, referenced by a net_node row.
		nodeStmt, err := sqlair.Prepare(`INSERT INTO "net_node" (uuid) VALUES ('net-node-0')`)
		if err != nil {
			return err
		}
		if err := tx.Query(ctx, nodeStmt).Run(); err != nil {
			return err
		}
		machineStmt, err := sqlair.Prepare(`INSERT INTO "machine" (uuid, name, net_node_uuid, life_id, password_hash, password_hash_algorithm_id) VALUES ('machine-0', '0', 'net-node-0', 0, 'target-machine-hash', '0')`)
		if err != nil {
			return err
		}
		return errors.Capture(tx.Query(ctx, machineStmt).Run())
	})
	c.Assert(err, tc.ErrorIsNil)
}

func blobMeta(content []byte) (sha256Hex, sha384Hex string, size int64) {
	s256 := sha256.Sum256(content)
	s384 := sha512.Sum384(content)
	return hex.EncodeToString(s256[:]), hex.EncodeToString(s384[:]), int64(len(content))
}

// buildArchive writes a backup archive: metadata.json, root.tar (with blob
// contents), controller.yaml, and one model dump per source model.
func buildArchive(c *tc.C, path string, ctrl *ctrlv4_1_0.ControllerExport, models map[string]*modelv4_1_0.ModelExport, blobs map[string][]byte) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	writeEntry := func(name string, content []byte) {
		c.Check(tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}), tc.ErrorIsNil)
		_, err := tw.Write(content)
		c.Check(err, tc.ErrorIsNil)
	}

	// Structural directory entries emitted by the backup writer.
	c.Check(tw.WriteHeader(&tar.Header{Name: "juju-backup", Mode: 0o700, Typeflag: tar.TypeDir}), tc.ErrorIsNil)
	c.Check(tw.WriteHeader(&tar.Header{Name: "juju-backup/dump", Mode: 0o700, Typeflag: tar.TypeDir}), tc.ErrorIsNil)
	c.Check(tw.WriteHeader(&tar.Header{Name: "juju-backup/dump/models", Mode: 0o700, Typeflag: tar.TypeDir}), tc.ErrorIsNil)

	// Metadata.
	meta := corebackups.NewMetadata()
	meta.Origin.Version = domainexport.LatestControllerExportVersion()
	meta.Controller = corebackups.ControllerMetadata{HANodes: 1}
	metaR, err := meta.AsJSONBuffer()
	c.Assert(err, tc.ErrorIsNil)
	metaBytes, err := io.ReadAll(metaR)
	c.Assert(err, tc.ErrorIsNil)
	writeEntry("juju-backup/metadata.json", metaBytes)

	// root.tar with blobs.
	var rbuf bytes.Buffer
	rtw := tar.NewWriter(&rbuf)
	for name, content := range blobs {
		c.Check(rtw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}), tc.ErrorIsNil)
		_, err := rtw.Write(content)
		c.Check(err, tc.ErrorIsNil)
	}
	c.Check(rtw.Close(), tc.ErrorIsNil)
	writeEntry("juju-backup/root.tar", rbuf.Bytes())

	// Controller dump.
	ctrlEnv := domainexport.ControllerExport{Version: domainexport.LatestControllerExportVersion(), Payload: ctrl}
	ctrlBytes, err := yaml.Marshal(ctrlEnv)
	c.Assert(err, tc.ErrorIsNil)
	writeEntry("juju-backup/dump/controller.yaml", ctrlBytes)

	// Model dumps.
	for uuid, m := range models {
		env := domainexport.ModelExport{Version: domainexport.LatestSupportedPayloadVersion(), Payload: m}
		b, err := yaml.Marshal(env)
		c.Assert(err, tc.ErrorIsNil)
		writeEntry("juju-backup/dump/models/"+uuid+".yaml", b)
	}

	c.Check(tw.Close(), tc.ErrorIsNil)
	c.Check(gw.Close(), tc.ErrorIsNil)
	c.Check(os.WriteFile(path, buf.Bytes(), 0o600), tc.ErrorIsNil)
}

// sourceControllerPayload is a minimal but valid source controller export:
// one cloud, one region, one credential, the controller model, its namespace
// mapping, and a blob reference (object store metadata + placement).
func sourceControllerPayload(sha256Hex, sha384Hex string, size int64) *ctrlv4_1_0.ControllerExport {
	return &ctrlv4_1_0.ControllerExport{
		Controller:  []ctrlv4_1_0.Controller{{UUID: srcCtrlUUID, ModelUUID: srcCtrlModelUUID, TargetVersion: "4.1.0", CaCert: new("source-ca-cert"), CaPrivateKey: new("source-ca-key"), Cert: new("source-cert"), PrivateKey: new("source-key"), SystemIdentity: new("source-identity")}},
		Cloud:       []ctrlv4_1_0.Cloud{{UUID: "src-cloud", Name: "dummy", CloudTypeID: 1}},
		CloudType:   []ctrlv4_1_0.CloudType{{ID: new(int64(1)), Type: "lxd"}},
		CloudRegion: []ctrlv4_1_0.CloudRegion{{UUID: "src-region", CloudUUID: "src-cloud", Name: "localhost"}},
		AuthType:    []ctrlv4_1_0.AuthType{{ID: new(int64(2)), Type: new("userpass")}},
		CloudCredential: []ctrlv4_1_0.CloudCredential{{
			UUID: "src-cred", CloudUUID: "src-cloud", AuthTypeID: "2", OwnerUUID: "user-1", Name: "default", Revoked: new(false),
		}},
		User: []ctrlv4_1_0.User{{
			UUID: "user-1", Name: "admin", External: false, Removed: false, CreatedByUUID: "user-1", CreatedAt: time.Unix(0, 0).UTC(),
		}},
		Model: []ctrlv4_1_0.Model{{
			UUID:                srcCtrlModelUUID,
			Activated:           true,
			CloudUUID:           "src-cloud",
			CloudRegionUUID:     new("src-region"),
			CloudCredentialUUID: new("src-cred"),
			ModelTypeID:         0,
			LifeID:              0,
			Name:                "controller",
			Qualifier:           "admin",
		}},
		ModelNamespace: []ctrlv4_1_0.ModelNamespace{{Namespace: srcCtrlModelUUID, ModelUUID: srcCtrlModelUUID}},
		ObjectStoreMetadata: []ctrlv4_1_0.ObjectStoreMetadata{{
			UUID: "blob-uuid", Sha256: sha256Hex, Sha384: sha384Hex, Size: size,
		}},
		ObjectStorePlacement: []ctrlv4_1_0.ObjectStorePlacement{{UUID: "blob-uuid", NodeID: "9"}},
	}
}

// sourceControllerModelPayload is the source controller model's model-DB
// export: the model row plus a source agent password.
func sourceControllerModelPayload() *modelv4_1_0.ModelExport {
	return &modelv4_1_0.ModelExport{
		Model: []modelv4_1_0.Model{{
			UUID:              srcCtrlModelUUID,
			ControllerUUID:    srcCtrlUUID,
			Name:              "controller",
			Qualifier:         "admin",
			Type:              "iaas",
			Cloud:             "dummy",
			CloudType:         "lxd",
			IsControllerModel: new(true),
		}},
		ModelAgent: []modelv4_1_0.ModelAgent{{
			ModelUUID:    srcCtrlModelUUID,
			PasswordHash: new("source-hash"),
		}},
		NetNode: []modelv4_1_0.NetNode{{UUID: "net-node-0"}},
		Machine: []modelv4_1_0.Machine{{
			UUID:         "machine-0",
			Name:         "0",
			NetNodeUUID:  "net-node-0",
			PasswordHash: new("source-machine-hash"),
		}},
	}
}

// writeAgentConf writes a minimal machine-0 agent.conf into dataDir, as
// bootstrap would have.
func writeAgentConf(c *tc.C, dataDir, controllerUUID, modelUUID string) {
	cfg, err := agent.NewAgentConfig(agent.AgentConfigParams{
		Paths:             agent.Paths{DataDir: dataDir},
		Jobs:              []model.MachineJob{model.JobHostUnits},
		Tag:               names.NewMachineTag("0"),
		UpgradedToVersion: domainexport.LatestControllerExportVersion(),
		Password:          "target-password",
		Controller:        names.NewControllerTag(controllerUUID),
		Model:             names.NewModelTag(modelUUID),
		APIAddresses:      []string{"localhost:17070"},
		CACert:            "target-ca-cert",
	})
	c.Assert(err, tc.ErrorIsNil)
	confPath := agent.ConfigPath(dataDir, names.NewMachineTag("0"))
	c.Assert(os.MkdirAll(filepath.Dir(confPath), 0700), tc.ErrorIsNil)
	err = cfg.Write()
	c.Assert(err, tc.ErrorIsNil)
}

// TestRestoreEndToEnd runs the full restore pipeline: preflight, staging,
// capture, compatibility check, namespace registration, model import,
// controller import + overlay, temp model deletion, and the object-store
// swap. It then verifies the restored source identities and target-local
// facts.
func (s *e2eSuite) TestRestoreEndToEnd(c *tc.C) {
	blob := []byte("blob-content")
	sha256Hex, sha384Hex, size := blobMeta(blob)

	archivePath := filepath.Join(c.MkDir(), "backup.tar.gz")
	buildArchive(c, archivePath, sourceControllerPayload(sha256Hex, sha384Hex, size),
		map[string]*modelv4_1_0.ModelExport{srcCtrlModelUUID: sourceControllerModelPayload()},
		map[string][]byte{"var/lib/juju/objectstore/" + srcCtrlModelUUID + "/" + sha384Hex: blob},
	)

	// Write the machine-0 agent.conf the restore regenerates.
	dataDir := c.MkDir()
	writeAgentConf(c, dataDir, tempCtrlUUID, tempCtrlModelNS)

	dir := c.MkDir()
	getter := &fakeDBGetter{c: c, suite: &s.ControllerModelSuite, opened: map[string]database.TxnRunner{}, registred: map[string]bool{tempCtrlModelNS: true}}
	deleter := &fakeDBDeleter{}
	r := &Restorer{
		DBGetter:        getter,
		DBDeleter:       deleter,
		StageDir:        filepath.Join(dir, "stage"),
		ObjectStoreRoot: filepath.Join(dir, "objectstore"),
		DataDir:         dataDir,
		Logger:          internallogger.Noop(),
	}
	err := r.Restore(c.Context(), archivePath)
	c.Assert(err, tc.ErrorIsNil)

	// The source controller identity is restored.
	var ctrlUUID, ctrlModelUUID string
	err = s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT uuid, model_uuid FROM "controller"`).Scan(&ctrlUUID, &ctrlModelUUID)
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(ctrlUUID, tc.Equals, srcCtrlUUID)
	c.Check(ctrlModelUUID, tc.Equals, srcCtrlModelUUID)

	// The temporary controller model is gone from the controller DB; the
	// source model remains.
	err = s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM "model"`).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return errors.New("expected exactly one model row")
		}
		var uuid string
		return tx.QueryRowContext(ctx, `SELECT uuid FROM "model"`).Scan(&uuid)
	})
	c.Assert(err, tc.ErrorIsNil)

	// namespace_list is authoritative: no temporary namespace remains, and
	// the controller namespace survives.
	err = s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM "namespace_list" WHERE namespace = ?`, tempCtrlModelNS).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return errors.New("temporary namespace not removed")
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM "namespace_list" WHERE namespace IN ('controller', ?)`, srcCtrlModelUUID).Scan(&n); err != nil {
			return err
		}
		if n != 2 {
			return errors.New("expected controller and source controller-model namespaces")
		}
		return nil
	})
	c.Assert(err, tc.ErrorIsNil)

	// The temporary controller-model namespace DB was deleted.
	c.Check(deleter.deleted, tc.DeepEquals, []string{tempCtrlModelNS})

	// The blob was staged, swapped, and its metadata restored.
	stagedBlob := filepath.Join(dir, "objectstore", srcCtrlModelUUID, sha384Hex)
	content, err := os.ReadFile(stagedBlob)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(content, tc.DeepEquals, blob)

	// Placement was rewritten to node 0.
	var nodeID string
	err = s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT node_id FROM "object_store_placement" WHERE uuid = 'blob-uuid'`).Scan(&nodeID)
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(nodeID, tc.Equals, "0")

	// The controller-model model_agent carries the target password hash, and
	// machine-0 carries the target machine-0 password material.
	srcModelDB := getter.opened[srcCtrlModelUUID]
	c.Assert(srcModelDB, tc.NotNil)
	var hash string
	err = srcModelDB.StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT password_hash FROM "model_agent"`).Scan(&hash)
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(hash, tc.Equals, "target-hash")
	var mhash string
	err = srcModelDB.StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT password_hash FROM "machine" WHERE name = '0'`).Scan(&mhash)
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(mhash, tc.Equals, "target-machine-hash")

	// The lease cleared.
	var leaseCount int
	err = s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM "lease"`).Scan(&leaseCount)
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(leaseCount, tc.Equals, 0)

	// The agent config was regenerated with the source identity.
	conf, err := agent.ReadConfig(agent.ConfigPath(dataDir, names.NewMachineTag("0")))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(conf.Controller().Id(), tc.Equals, srcCtrlUUID)
	c.Check(conf.Model().Id(), tc.Equals, srcCtrlModelUUID)
	srcCaCert := ""
	if src := sourceControllerPayload("", "", 0).Controller[0].CaCert; src != nil {
		srcCaCert = *src
	}
	if srcCaCert != "" {
		c.Check(conf.CACert(), tc.Equals, srcCaCert)
	}
}

// TestRestoreFailsBeforeWritesOnUnsupportedSource asserts a source archive
// violating the support gate fails in preflight, before any mutation.
func (s *e2eSuite) TestRestoreFailsBeforeWritesOnUnsupportedSource(c *tc.C) {
	archivePath := filepath.Join(c.MkDir(), "backup.tar.gz")
	ctrl := sourceControllerPayload("", "", 0)
	ctrl.ObjectStoreBackendS3Config = []ctrlv4_1_0.ObjectStoreBackendS3Config{{ObjectStoreBackendUUID: "s3", Endpoint: "https://s3"}}
	buildArchive(c, archivePath, ctrl,
		map[string]*modelv4_1_0.ModelExport{srcCtrlModelUUID: sourceControllerModelPayload()},
		map[string][]byte{},
	)

	dir := c.MkDir()
	getter := &fakeDBGetter{c: c, suite: &s.ControllerModelSuite, opened: map[string]database.TxnRunner{}, registred: map[string]bool{tempCtrlModelNS: true}}
	r := &Restorer{
		DBGetter:        getter,
		DBDeleter:       &fakeDBDeleter{},
		StageDir:        filepath.Join(dir, "stage"),
		ObjectStoreRoot: filepath.Join(dir, "objectstore"),
		Logger:          internallogger.Noop(),
	}
	err := r.Restore(c.Context(), archivePath)
	c.Assert(err, tc.ErrorIs, ErrSourceUnsupported)

	// No mutation: the controller table still carries the target identity.
	var uuid string
	err = s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT uuid FROM "controller"`).Scan(&uuid)
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(uuid, tc.Equals, tempCtrlUUID)
}

// TestRestoreFailsOnIncompatibleTarget asserts a non-fresh target fails
// before any database mutation.
func (s *e2eSuite) TestRestoreFailsOnIncompatibleTarget(c *tc.C) {
	// Make the target incompatible: add a second model row.
	err := s.TxnRunner().StdTxn(c.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO model (uuid, activated, cloud_uuid, model_type_id, life_id, name, qualifier) VALUES ('extra-model', 1, 'temp-cloud', 0, 0, 'extra', 'admin')`)
		return err
	})
	c.Assert(err, tc.ErrorIsNil)

	blob := []byte("blob-content")
	sha256Hex, sha384Hex, size := blobMeta(blob)
	archivePath := filepath.Join(c.MkDir(), "backup.tar.gz")
	buildArchive(c, archivePath, sourceControllerPayload(sha256Hex, sha384Hex, size),
		map[string]*modelv4_1_0.ModelExport{srcCtrlModelUUID: sourceControllerModelPayload()},
		map[string][]byte{"var/lib/juju/objectstore/" + srcCtrlModelUUID + "/" + sha384Hex: blob},
	)

	dir := c.MkDir()
	getter := &fakeDBGetter{c: c, suite: &s.ControllerModelSuite, opened: map[string]database.TxnRunner{}, registred: map[string]bool{tempCtrlModelNS: true}}
	r := &Restorer{
		DBGetter:        getter,
		DBDeleter:       &fakeDBDeleter{},
		StageDir:        filepath.Join(dir, "stage"),
		ObjectStoreRoot: filepath.Join(dir, "objectstore"),
		Logger:          internallogger.Noop(),
	}
	err = r.Restore(c.Context(), archivePath)
	c.Assert(err, tc.ErrorIs, ErrTargetIncompatible)
}
