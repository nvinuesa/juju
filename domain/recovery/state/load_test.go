// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state_test

import (
	"context"
	"database/sql"
	stdtesting "testing"

	"github.com/juju/tc"

	"github.com/juju/juju/core/database"
	"github.com/juju/juju/core/network"
	networkstate "github.com/juju/juju/domain/network/state"
	domainrecovery "github.com/juju/juju/domain/recovery"
	recoverystate "github.com/juju/juju/domain/recovery/state"
	"github.com/juju/juju/domain/schema"
	schematesting "github.com/juju/juju/domain/schema/testing"
	loggertesting "github.com/juju/juju/internal/logger/testing"
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

// seedTargetController simulates what bootstrap wrote before the recovery
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

func (s *loadSuite) controllerDump() *domainrecovery.Dump {
	return &domainrecovery.Dump{
		Version: "4.1.0",
		Tables: map[string][]domainrecovery.Row{
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
			// Lookup tables are replaced: the archive carries every
			// seeded row (idempotent under the exact-version gate).
			"life": {
				{"id": int64(0), "value": "alive"},
				{"id": int64(1), "value": "dying"},
				{"id": int64(2), "value": "dead"},
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

	err := recoverystate.LoadControllerDump(c.Context(), s.DB(), s.controllerDump())
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

	// Cloud and model rows are recovered.
	var cloudName string
	err = s.DB().QueryRow("SELECT name FROM cloud WHERE uuid = ?", sourceCloudUUID).Scan(&cloudName)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(cloudName, tc.Equals, "lxd")

	// Lookup rows are replaced: the dump carries the full set
	// (idempotent under the exact-version gate).
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
	// surviving metadata row. The fresh bootstrap blob row is replaced
	// by the archived one and its placement is rebuilt; an orphaned
	// on-disk file is handled by LoadObjects later.
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
		sourceBlobUUID: "bootstrap-node",
	})
}

func (s *loadSuite) TestLoadControllerDumpForeignKeyViolation(c *tc.C) {
	dump := s.controllerDump()
	dump.Tables["model"] = []domainrecovery.Row{{
		"uuid":          sourceModelUUID,
		"activated":     true,
		"cloud_uuid":    "99999999-0000-0000-0000-000000000000",
		"model_type_id": int64(0),
		"life_id":       int64(0),
		"name":          "broken",
		"qualifier":     "",
	}}
	delete(dump.Tables, "cloud")

	err := recoverystate.LoadControllerDump(c.Context(), s.DB(), dump)
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

func (s *loadSuite) modelDump() *domainrecovery.Dump {
	return &domainrecovery.Dump{
		Version: "4.1.0",
		Tables: map[string][]domainrecovery.Row{
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
			// Spaces are replaced: the archive carries every row,
			// including the deterministic alpha seed.
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

	err := recoverystate.LoadModelDump(c.Context(), db, s.modelDump())
	c.Assert(err, tc.ErrorIsNil)

	var machineName string
	err = db.QueryRow("SELECT name FROM machine WHERE uuid = ?", "machine-uuid-0").Scan(&machineName)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(machineName, tc.Equals, "0")

	// Spaces are replaced: the archive includes the deterministic alpha
	// seed and the user-created space.
	var spaceCount int
	err = db.QueryRow("SELECT COUNT(*) FROM space").Scan(&spaceCount)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(spaceCount, tc.Equals, 2)
}

func (s *loadSuite) TestPatchControllerMachine(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()
	c.Assert(recoverystate.LoadModelDump(c.Context(), db, s.modelDump()), tc.ErrorIsNil)

	err := recoverystate.PatchControllerMachine(c.Context(), db, domainrecovery.MachinePatch{
		MachineName: "0",
		InstanceID:  "replacement-instance",
		DisplayName: "replacement-display",
		Arch:        "amd64",
		MemMB:       8192,
		Nonce:       "replacement-nonce",
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
	var nonce string
	c.Assert(db.QueryRow("SELECT nonce FROM machine WHERE name = '0'").Scan(&nonce), tc.ErrorIsNil)
	c.Check(nonce, tc.Equals, "replacement-nonce")
}

func (s *loadSuite) TestPatchControllerMachineMissing(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()
	c.Assert(recoverystate.LoadModelDump(c.Context(), db, s.modelDump()), tc.ErrorIsNil)

	err := recoverystate.PatchControllerMachine(c.Context(), db, domainrecovery.MachinePatch{
		MachineName: "9",
		InstanceID:  "x",
	})
	c.Assert(err, tc.ErrorMatches, `archived controller machine "9" not found`)
}

func (s *loadSuite) TestLoadControllerDumpRejectsUnknownVersion(c *tc.C) {
	s.seedTargetController(c)

	dump := s.controllerDump()
	dump.Version = "3.6.0"
	err := recoverystate.LoadControllerDump(c.Context(), s.DB(), dump)
	c.Assert(err, tc.ErrorMatches, "controller dump: unsupported dump format version \"3.6.0\".*")

	dump.Version = ""
	err = recoverystate.LoadControllerDump(c.Context(), s.DB(), dump)
	c.Assert(err, tc.ErrorMatches, "controller dump: dump records no format version")
}

func (s *loadSuite) TestLoadControllerDiscardsBackupLease(c *tc.C) {
	s.seedTargetController(c)
	dump := s.controllerDump()
	dump.Tables["lease_type"] = []domainrecovery.Row{
		{"id": 0, "type": "singular-controller"},
		{"id": 1, "type": "application-leadership"},
		{"id": 2, "type": "backup-creation"},
	}
	dump.Tables["lease"] = []domainrecovery.Row{
		{"uuid": "backup", "lease_type_id": 2, "holder": "request", "model_uuid": sourceModelUUID},
		{"uuid": "leadership", "lease_type_id": 1, "holder": "app/0", "model_uuid": sourceModelUUID},
	}
	dump.Tables["lease_pin"] = []domainrecovery.Row{
		{"uuid": "backup-pin", "lease_uuid": "backup", "entity_id": "request"},
		{"uuid": "leader-pin", "lease_uuid": "leadership", "entity_id": "unit"},
	}
	err := recoverystate.LoadControllerDump(c.Context(), s.DB(), dump)
	c.Assert(err, tc.ErrorIsNil)
	var holder string
	err = s.DB().QueryRowContext(c.Context(), "SELECT holder FROM lease").Scan(&holder)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(holder, tc.Equals, "app/0")
	var pin string
	err = s.DB().QueryRowContext(c.Context(), "SELECT uuid FROM lease_pin").Scan(&pin)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(pin, tc.Equals, "leader-pin")
	c.Check(dump.Tables["lease"], tc.HasLen, 2)
	var leaseType string
	err = s.DB().QueryRowContext(c.Context(), "SELECT type FROM lease_type WHERE id = 2").Scan(&leaseType)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(leaseType, tc.Equals, "backup-creation")
}

func (s *loadSuite) TestLoadModelDumpRejectsUnknownVersion(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()

	dump := s.modelDump()
	dump.Version = "4.2.0"
	err := recoverystate.LoadModelDump(c.Context(), db, dump)
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
	// one: the old observation is replaced and the device is retained.
	patch, err := domainrecovery.NewControllerUnitAddressPatch([]string{"10.1.0.1", "10.1.0.2"})
	c.Assert(err, tc.ErrorIsNil)
	err = recoverystate.PatchControllerUnitAddresses(c.Context(), db, patch)
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
	c.Check(rows[0].addressValue, tc.Equals, "10.1.0.1")
	c.Check(rows[1].addressValue, tc.Equals, "10.1.0.2")
	c.Check(rows[1].uuid, tc.Not(tc.Equals), "ip-uuid-0")
	c.Check(rows[1].deviceUUID, tc.Equals, "lld-uuid-0")
}

func (s *loadSuite) TestPatchControllerUnitAddressesRepresentations(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()
	s.seedControllerUnitApplication(c, db)
	insert := func(query string, args ...any) {
		_, err := db.ExecContext(c.Context(), query, args...)
		c.Assert(err, tc.ErrorIsNil)
	}
	insert(`INSERT INTO model
		(uuid, controller_uuid, name, qualifier, type, cloud, cloud_type, is_controller_model)
		VALUES (?, ?, 'controller', 'admin', 'caas', 'test', 'kubernetes', true)`,
		sourceModelUUID, sourceControllerUUID)
	insert("INSERT INTO net_node (uuid) VALUES ('service-node')")
	insert(`INSERT INTO k8s_service (uuid, application_uuid, net_node_uuid, provider_id)
		VALUES ('service', 'app-uuid-0', 'service-node', 'source-service')`)
	insert(`INSERT INTO fqdn_address (uuid, address, scope_id) VALUES
		('shared', 'shared.example.com', 1), ('old', 'old.example.com', 1)`)
	insert(`INSERT INTO net_node_fqdn_address (net_node_uuid, address_uuid) VALUES
		('nn-0', 'shared'), ('service-node', 'shared'), ('nn-0', 'old')`)
	runner := s.ModelTxnRunner(c, sourceModelUUID)
	consumer := networkstate.NewState(func(context.Context) (database.TxnRunner, error) {
		return runner, nil
	}, loggertesting.WrapCheckLog(c))
	for _, values := range [][]string{
		{"pod.example.com", "10.1.0.1", "2001:db8::1", "pod.example.com"},
		{"next.example.com"},
		{"10.1.0.2", "2001:db8::2"},
	} {
		patch, err := domainrecovery.NewControllerUnitAddressPatch(values)
		c.Assert(err, tc.ErrorIsNil)
		err = recoverystate.PatchControllerUnitAddresses(c.Context(), db, patch)
		c.Assert(err, tc.ErrorIsNil)
		observed, err := consumer.GetControllerUnitNetwork(c.Context(), "controller/0")
		c.Assert(err, tc.ErrorIsNil)
		actual, expected := make(map[string]network.AddressType), make(map[string]network.AddressType)
		for _, address := range observed {
			actual[address.Value] = address.Type
			c.Check(address.Scope, tc.Equals, network.ScopeCloudLocal)
		}
		for _, value := range values {
			expected[value] = network.DeriveAddressType(value)
		}
		c.Check(actual, tc.DeepEquals, expected)
		service, err := consumer.GetControllerServiceAddresses(c.Context())
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(service, tc.HasLen, 1)
		c.Check(service[0].Value, tc.Equals, "shared.example.com")
	}
	var stale int
	err := db.QueryRowContext(c.Context(),
		"SELECT COUNT(*) FROM fqdn_address WHERE address IN ('old.example.com', 'pod.example.com', 'next.example.com')").Scan(&stale)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(stale, tc.Equals, 0)
}

func (s *loadSuite) TestPatchControllerUnitAddressesWithoutArchivedDevice(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()
	s.seedControllerUnitApplication(c, db)
	_, err := db.ExecContext(c.Context(), "DELETE FROM ip_address")
	c.Assert(err, tc.ErrorIsNil)
	_, err = db.ExecContext(c.Context(), "DELETE FROM link_layer_device")
	c.Assert(err, tc.ErrorIsNil)
	patch, err := domainrecovery.NewControllerUnitAddressPatch([]string{"pod.example.com"})
	c.Assert(err, tc.ErrorIsNil)
	err = recoverystate.PatchControllerUnitAddresses(c.Context(), db, patch)
	c.Assert(err, tc.ErrorIsNil)
	patch, err = domainrecovery.NewControllerUnitAddressPatch([]string{"10.1.0.1"})
	c.Assert(err, tc.ErrorIsNil)
	err = recoverystate.PatchControllerUnitAddresses(c.Context(), db, patch)
	c.Assert(err, tc.ErrorIsNil)
	var deviceUUID string
	err = db.QueryRowContext(c.Context(),
		"SELECT device_uuid FROM ip_address WHERE net_node_uuid = 'nn-0'").Scan(&deviceUUID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(deviceUUID, tc.Equals, patch.DeviceUUID)
	patch.Addresses[0].UUID = ""
	patch.Addresses = append(patch.Addresses, patch.Addresses[0])
	err = recoverystate.PatchControllerUnitAddresses(c.Context(), db, patch)
	c.Assert(err, tc.ErrorMatches, ".*UNIQUE constraint failed.*")
	// A failed replacement retains the previously committed observations.
	err = db.QueryRowContext(c.Context(), "SELECT device_uuid FROM ip_address").Scan(&deviceUUID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(deviceUUID, tc.Equals, patch.DeviceUUID)
}

func (s *loadSuite) TestPatchControllerUnitAddressesMissingUnit(c *tc.C) {
	db := s.openModelDB(c, sourceModelUUID)
	defer db.Close()
	patch, err := domainrecovery.NewControllerUnitAddressPatch(nil)
	c.Assert(err, tc.ErrorIsNil)
	err = recoverystate.PatchControllerUnitAddresses(c.Context(), db, patch)
	c.Assert(err, tc.ErrorIsNil)
	_, err = domainrecovery.NewControllerUnitAddressPatch([]string{""})
	c.Assert(err, tc.ErrorMatches, "empty controller unit address")
	patch, err = domainrecovery.NewControllerUnitAddressPatch([]string{"pod.example.com"})
	c.Assert(err, tc.ErrorIsNil)
	err = recoverystate.PatchControllerUnitAddresses(c.Context(), db, patch)
	c.Assert(err, tc.ErrorMatches, "controller unit not found")
}
