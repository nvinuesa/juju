// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore_test

import (
	"database/sql"
	stdtesting "testing"

	"github.com/juju/tc"

	"github.com/juju/juju/domain/schema"
	schematesting "github.com/juju/juju/domain/schema/testing"
	"github.com/juju/juju/internal/restore"
)

type loadSuite struct {
	schematesting.ControllerModelSuite
}

func TestLoadSuite(t *stdtesting.T) {
	tc.Run(t, &loadSuite{})
}

const (
	targetControllerUUID = "11111111-0000-0000-0000-000000000000"
	sourceControllerUUID = "22222222-0000-0000-0000-000000000000"
	sourceModelUUID      = "33333333-0000-0000-0000-000000000000"
	sourceCloudUUID      = "44444444-0000-0000-0000-000000000000"
	targetBlobUUID       = "55555555-0000-0000-0000-000000000000"
	sourceBlobUUID       = "66666666-0000-0000-0000-000000000000"
)

// seedTargetController simulates what bootstrap wrote before the restore
// load runs: the controller row, and one object blob with its placement.
func (s *loadSuite) seedTargetController(c *tc.C) {
	_, err := s.DB().Exec(
		"INSERT INTO controller (uuid, model_uuid, target_version) VALUES (?, ?, ?)",
		targetControllerUUID, "00000000-0000-0000-0000-000000000000", "4.1.0")
	c.Assert(err, tc.ErrorIsNil)
	_, err = s.DB().Exec(
		"INSERT INTO object_store_metadata (uuid, sha_256, sha_384, size) VALUES (?, ?, ?, ?)",
		targetBlobUUID, "aa", "bb", 2)
	c.Assert(err, tc.ErrorIsNil)
	_, err = s.DB().Exec(
		"INSERT INTO object_store_placement (uuid, node_id) VALUES (?, ?)",
		targetBlobUUID, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)
}

func (s *loadSuite) controllerDump() *restore.Dump {
	return &restore.Dump{
		Version: "4.1.0",
		Tables: map[string][]restore.Row{
			"controller": {{
				"uuid":           sourceControllerUUID,
				"model_uuid":     sourceModelUUID,
				"target_version": "4.1.0",
				"api_port":       "17070",
				"ca_cert":        "source-ca",
			}},
			"cloud": {{
				"uuid":            sourceCloudUUID,
				"name":            "lxd",
				"cloud_type_id":   int64(1),
				"endpoint":        "",
				"skip_tls_verify": false,
			}},
			"model": {{
				"uuid":          sourceModelUUID,
				"activated":     true,
				"cloud_uuid":    sourceCloudUUID,
				"model_type_id": int64(0),
				"life_id":       int64(0),
				"name":          "controller",
				"qualifier":     "",
			}},
			"controller_config": {{
				"key":   "controller-name",
				"value": "source-ctrl",
			}},
			// Binary column values appear in dumps in two shapes:
			// []byte-typed export fields marshal as sequences of
			// integers, and string-typed fields holding binary marshal
			// as !!binary, which the YAML decoder returns as a string
			// of raw bytes. Both load back as BLOB storage.
			"controller_ssh_host_key": {{
				"id":                "host-key-uuid",
				"algorithm_type_id": int64(2),
				"ssh_key":           "-----BEGIN OPENSSH PRIVATE KEY-----",
				"public_key":        []any{int(0), int(0), int(0), int(11), int(115)},
			}},
			"bakery_config": {{
				"local_users_private_key":                "\x00\xff\x02",
				"local_users_public_key":                 "\x00\xff\x02",
				"local_users_third_party_private_key":    "\x00\xff\x02",
				"local_users_third_party_public_key":     "\x00\xff\x02",
				"external_users_third_party_private_key": "\x00\xff\x02",
				"external_users_third_party_public_key":  "\x00\xff\x02",
				"offers_third_party_private_key":         "\x00\xff\x02",
				"offers_third_party_public_key":          "\x00\xff\x02",
			}},
			"object_store_metadata": {{
				"uuid":    sourceBlobUUID,
				"sha_256": "cc",
				"sha_384": "dd",
				"size":    int64(2),
			}},
			"object_store_placement": {{
				"uuid":    sourceBlobUUID,
				"node_id": "source-node-5",
			}},
			// Seeds and runtime state in the dump must be skipped:
			// identical lookups, stale node rows.
			"life": {
				{"id": int64(0), "value": "alive"},
			},
			"controller_node": {{
				"controller_id":       "99",
				"dqlite_node_id":      int64(1234),
				"dqlite_bind_address": "10.0.0.9",
			}},
		},
	}
}

func (s *loadSuite) TestLoadControllerDump(c *tc.C) {
	s.seedTargetController(c)

	err := restore.LoadControllerDump(c.Context(), s.DB(), s.controllerDump())
	c.Assert(err, tc.ErrorIsNil)

	// The controller row carries the source identity.
	var uuid, caCert string
	err = s.DB().QueryRow("SELECT uuid, ca_cert FROM controller").Scan(&uuid, &caCert)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(uuid, tc.Equals, sourceControllerUUID)
	c.Check(caCert, tc.Equals, "source-ca")

	// The binary column survived as BLOB storage with its raw bytes.
	var publicKey []byte
	err = s.DB().QueryRow(
		"SELECT public_key FROM controller_ssh_host_key WHERE id = ?",
		"host-key-uuid").Scan(&publicKey)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(publicKey, tc.DeepEquals, []byte{0, 0, 0, 11, 115})

	// The !!binary-decoded string fields load as BLOB storage, ready
	// for byte-expecting scanners like the bakery key scanner.
	var keyClass string
	err = s.DB().QueryRow(
		"SELECT typeof(local_users_third_party_private_key) FROM bakery_config",
	).Scan(&keyClass)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(keyClass, tc.Equals, "blob")

	// Cloud and model rows are restored.
	var cloudName string
	err = s.DB().QueryRow("SELECT name FROM cloud WHERE uuid = ?", sourceCloudUUID).Scan(&cloudName)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(cloudName, tc.Equals, "lxd")

	// Lookup seeds are not duplicated.
	var lifeRows int
	err = s.DB().QueryRow("SELECT COUNT(*) FROM life").Scan(&lifeRows)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(lifeRows, tc.Equals, 3)

	// The replacement's controller node row is untouched.
	var nodeID int64
	err = s.DB().QueryRow(
		"SELECT dqlite_node_id FROM controller_node").Scan(&nodeID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(nodeID, tc.Equals, int64(schematesting.DqliteNodeID))

	// Placements are rebuilt onto the replacement's node for every
	// metadata row, including the merged-in source blob.
	rows, err := s.DB().Query("SELECT uuid, node_id FROM object_store_placement ORDER BY uuid")
	c.Assert(err, tc.ErrorIsNil)
	defer rows.Close()
	placements := map[string]string{}
	for rows.Next() {
		var u, n string
		c.Assert(rows.Scan(&u, &n), tc.ErrorIsNil)
		placements[u] = n
	}
	c.Assert(rows.Err(), tc.ErrorIsNil)
	c.Check(placements, tc.DeepEquals, map[string]string{
		targetBlobUUID: "bootstrap-node",
		sourceBlobUUID: "bootstrap-node",
	})
}

func (s *loadSuite) TestLoadControllerDumpForeignKeyViolation(c *tc.C) {
	dump := s.controllerDump()
	dump.Tables["model"] = []restore.Row{{
		"uuid":          sourceModelUUID,
		"activated":     true,
		"cloud_uuid":    "99999999-0000-0000-0000-000000000000",
		"model_type_id": int64(0),
		"life_id":       int64(0),
		"name":          "broken",
		"qualifier":     "",
	}}
	delete(dump.Tables, "cloud")

	err := restore.LoadControllerDump(c.Context(), s.DB(), dump)
	c.Assert(err, tc.ErrorMatches, ".*fails foreign key integrity.*")

	// The failed load rolled back: no model row remains.
	var count int
	err = s.DB().QueryRow("SELECT COUNT(*) FROM model").Scan(&count)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(count, tc.Equals, 0)
}

func (s *loadSuite) openModelDB(c *tc.C, modelUUID string) *sql.DB {
	runner, db := s.OpenDBForNamespace(c, modelUUID, true)
	s.DqliteSuite.ApplyDDLForRunner(c, &schematesting.SchemaApplier{
		Schema: schema.ModelDDL(),
	}, runner)
	return db
}

func (s *loadSuite) modelDump() *restore.Dump {
	return &restore.Dump{
		Version: "4.1.0",
		Tables: map[string][]restore.Row{
			"net_node": {{"uuid": "nn-0"}},
			"machine": {{
				"uuid":          "machine-uuid-0",
				"name":          "0",
				"net_node_uuid": "nn-0",
				"life_id":       int64(0),
			}},
			"machine_cloud_instance": {{
				"machine_uuid": "machine-uuid-0",
				"life_id":      int64(0),
				"instance_id":  "source-instance",
				"display_name": "source-display",
			}},
			// The seeded alpha space plus one user space.
			"space": {
				{"uuid": "656b4a82-e28c-53d6-a014-f0dd53417eb6", "name": "alpha"},
				{"uuid": "77777777-0000-0000-0000-000000000000", "name": "dmz"},
			},
		},
	}
}

func (s *loadSuite) TestLoadModelDump(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()

	err := restore.LoadModelDump(c.Context(), db, s.modelDump())
	c.Assert(err, tc.ErrorIsNil)

	var machineName string
	err = db.QueryRow("SELECT name FROM machine WHERE uuid = ?", "machine-uuid-0").Scan(&machineName)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(machineName, tc.Equals, "0")

	// The alpha space merged without duplication; the user space loaded.
	var spaceCount int
	err = db.QueryRow("SELECT COUNT(*) FROM space").Scan(&spaceCount)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(spaceCount, tc.Equals, 2)
}

func (s *loadSuite) TestPatchControllerMachine(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()
	c.Assert(restore.LoadModelDump(c.Context(), db, s.modelDump()), tc.ErrorIsNil)

	err := restore.PatchControllerMachine(c.Context(), db, restore.MachinePatch{
		MachineName: "0",
		InstanceID:  "replacement-instance",
		DisplayName: "replacement-display",
		Arch:        "amd64",
		MemMB:       8192,
	})
	c.Assert(err, tc.ErrorIsNil)

	var instanceID, displayName, arch string
	var mem int64
	err = db.QueryRow(
		"SELECT instance_id, display_name, arch, mem FROM machine_cloud_instance WHERE machine_uuid = ?",
		"machine-uuid-0").Scan(&instanceID, &displayName, &arch, &mem)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(instanceID, tc.Equals, "replacement-instance")
	c.Check(displayName, tc.Equals, "replacement-display")
	c.Check(arch, tc.Equals, "amd64")
	c.Check(mem, tc.Equals, int64(8192))
}

func (s *loadSuite) TestPatchControllerMachineMissing(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()
	c.Assert(restore.LoadModelDump(c.Context(), db, s.modelDump()), tc.ErrorIsNil)

	err := restore.PatchControllerMachine(c.Context(), db, restore.MachinePatch{
		MachineName: "9",
		InstanceID:  "x",
	})
	c.Assert(err, tc.ErrorMatches, `archived controller machine "9" not found`)
}

func (s *loadSuite) TestLoadControllerDumpRejectsUnknownVersion(c *tc.C) {
	s.seedTargetController(c)

	dump := s.controllerDump()
	dump.Version = "3.6.0"
	err := restore.LoadControllerDump(c.Context(), s.DB(), dump)
	c.Assert(err, tc.ErrorMatches, "controller dump: unsupported dump format version \"3.6.0\".*")

	dump.Version = ""
	err = restore.LoadControllerDump(c.Context(), s.DB(), dump)
	c.Assert(err, tc.ErrorMatches, "controller dump: dump records no format version")
}

func (s *loadSuite) TestLoadModelDumpRejectsUnknownVersion(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()

	dump := s.modelDump()
	dump.Version = "4.2.0"
	err := restore.LoadModelDump(c.Context(), db, dump)
	c.Assert(err, tc.ErrorMatches, "model dump: unsupported dump format version \"4.2.0\".*")
}

// seedControllerUnitApplication seeds the model tables a controller
// application and unit need, including one observed ip_address row.
func (s *loadSuite) seedControllerUnitApplication(c *tc.C, db *sql.DB) {
	insert := func(query string, args ...any) {
		_, err := db.Exec(query, args...)
		c.Assert(err, tc.ErrorIsNil)
	}
	insert("INSERT INTO net_node (uuid) VALUES (?)", "nn-0")
	insert(`INSERT INTO charm (uuid, reference_name, revision, source_id, architecture_id)
		VALUES (?, 'controller', 1, 1, 0)`, "charm-uuid-0")
	insert(`INSERT INTO application (uuid, name, life_id, charm_uuid, space_uuid)
		VALUES (?, 'controller', 0, 'charm-uuid-0', '656b4a82-e28c-53d6-a014-f0dd53417eb6')`, "app-uuid-0")
	insert("INSERT INTO application_controller (application_uuid) VALUES (?)", "app-uuid-0")
	insert(`INSERT INTO unit (uuid, name, life_id, application_uuid, net_node_uuid, charm_uuid)
		VALUES (?, 'controller/0', 0, 'app-uuid-0', 'nn-0', 'charm-uuid-0')`, "unit-uuid-0")
	insert(`INSERT INTO link_layer_device (uuid, net_node_uuid, name, device_type_id, virtual_port_type_id)
		VALUES (?, 'nn-0', 'eth0', 2, 0)`, "lld-uuid-0")
	insert(`INSERT INTO ip_address (uuid, net_node_uuid, device_uuid, address_value, type_id, config_type_id, origin_id, scope_id)
		VALUES (?, 'nn-0', 'lld-uuid-0', '10.0.0.5/24', 0, 0, 0, 0)`, "ip-uuid-0")
}

func (s *loadSuite) TestPatchControllerUnitAddressesInsertsMissing(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()
	s.seedControllerUnitApplication(c, db)

	// The replacement exposes two addresses but the archive recorded
	// one: the surplus is inserted, cloning the archived row's device
	// and classification.
	err := restore.PatchControllerUnitAddresses(c.Context(), db,
		[]string{"10.1.0.1:17070", "10.1.0.2:17070"})
	c.Assert(err, tc.ErrorIsNil)

	type addrRow struct {
		uuid, addressValue, deviceUUID string
	}
	var rows []addrRow
	result, err := db.Query(
		"SELECT uuid, address_value, device_uuid FROM ip_address WHERE net_node_uuid = 'nn-0' ORDER BY address_value")
	c.Assert(err, tc.ErrorIsNil)
	defer result.Close()
	for result.Next() {
		var row addrRow
		c.Assert(result.Scan(&row.uuid, &row.addressValue, &row.deviceUUID), tc.ErrorIsNil)
		rows = append(rows, row)
	}
	c.Assert(result.Err(), tc.ErrorIsNil)
	c.Assert(rows, tc.HasLen, 2)
	c.Check(rows[0].addressValue, tc.Equals, "10.1.0.1:17070")
	c.Check(rows[1].addressValue, tc.Equals, "10.1.0.2:17070")
	c.Check(rows[1].uuid, tc.Not(tc.Equals), "ip-uuid-0")
	c.Check(rows[1].deviceUUID, tc.Equals, "lld-uuid-0")
}
