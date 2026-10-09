// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	stdtesting "testing"

	"github.com/juju/tc"

	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/domain/schema"
	schematesting "github.com/juju/juju/domain/schema/testing"
	"github.com/juju/juju/internal/errors"
	loggertesting "github.com/juju/juju/internal/logger/testing"
	"github.com/juju/juju/internal/recovery"
)

type loadSuite struct {
	schematesting.ControllerModelSuite
}

const sourceModelUUID = "33333333-0000-0000-0000-000000000000"

func TestLoadSuite(t *stdtesting.T) {
	tc.Run(t, &loadSuite{})
}

// openModelDB opens the model database with the model schema applied,
// mirroring the fresh database the stage's OpenModelDB produces.
func (s *loadSuite) openModelDB(c *tc.C, modelUUID string) *sql.DB {
	runner, db := s.OpenDBForNamespace(c, modelUUID, true)
	s.DqliteSuite.ApplyDDLForRunner(c, &schematesting.SchemaApplier{
		Schema: schema.ModelDDL(),
	}, runner)
	return db
}

// controllerModelDumpYAML is the controller model's database dump with the
// source controller application, unit and machine.
const controllerModelDumpYAML = `version: 4.1.0
payload:
  net_node:
  - uuid: nn-0
  link_layer_device:
  - uuid: lld-0
    net_node_uuid: nn-0
    name: eth0
    device_type_id: 2
    virtual_port_type_id: 0
  ip_address:
  - uuid: ip-uuid-0
    net_node_uuid: nn-0
    device_uuid: lld-0
    address_value: 10.0.0.5/24
    type_id: 0
    config_type_id: 0
    origin_id: 0
    scope_id: 0
  charm:
  - uuid: charm-uuid-0
    reference_name: controller
    revision: 1
    source_id: 1
    architecture_id: 0
  application:
  - uuid: app-uuid-0
    name: controller
    life_id: 0
    charm_uuid: charm-uuid-0
    space_uuid: 656b4a82-e28c-53d6-a014-f0dd53417eb6
  application_controller:
  - application_uuid: app-uuid-0
  unit:
  - uuid: unit-uuid-0
    name: controller/0
    life_id: 0
    application_uuid: app-uuid-0
    net_node_uuid: nn-0
    charm_uuid: charm-uuid-0
  machine:
  - uuid: machine-uuid-0
    name: "0"
    net_node_uuid: nn-0
    life_id: 0
  machine_cloud_instance:
  - machine_uuid: machine-uuid-0
    life_id: 0
    instance_id: source-instance
    display_name: source-display
`

const emptyModelDumpYAML = `version: 4.1.0
payload:
  net_node:
  - uuid: nn-x
`

// controllerModelDumpHAYAML is an HA controller model dump: machine "0"
// is dead (dead unit controller/0), machine "1" is the live controller
// machine. Neither carries the ordinal the replacement must assume.
const controllerModelDumpHAYAML = `version: 4.1.0
payload:
  net_node:
  - uuid: nn-0
  - uuid: nn-1
  link_layer_device:
  - uuid: lld-0
    net_node_uuid: nn-1
    name: eth0
    device_type_id: 2
    virtual_port_type_id: 0
  ip_address:
  - uuid: ip-uuid-0
    net_node_uuid: nn-1
    device_uuid: lld-0
    address_value: 10.0.0.5/24
    type_id: 0
    config_type_id: 0
    origin_id: 0
    scope_id: 0
  charm:
  - uuid: charm-uuid-0
    reference_name: controller
    revision: 1
    source_id: 1
    architecture_id: 0
  application:
  - uuid: app-uuid-0
    name: controller
    life_id: 0
    charm_uuid: charm-uuid-0
    space_uuid: 656b4a82-e28c-53d6-a014-f0dd53417eb6
  application_controller:
  - application_uuid: app-uuid-0
  unit:
  - uuid: unit-uuid-0
    name: controller/0
    life_id: 1
    application_uuid: app-uuid-0
    net_node_uuid: nn-0
    charm_uuid: charm-uuid-0
  - uuid: unit-uuid-1
    name: controller/1
    life_id: 0
    application_uuid: app-uuid-0
    net_node_uuid: nn-1
    charm_uuid: charm-uuid-0
  machine:
  - uuid: machine-uuid-0
    name: "0"
    net_node_uuid: nn-0
    life_id: 1
  - uuid: machine-uuid-1
    name: "1"
    net_node_uuid: nn-1
    life_id: 0
  machine_cloud_instance:
  - machine_uuid: machine-uuid-0
    life_id: 0
    instance_id: source-instance-0
    display_name: source-display-0
  - machine_uuid: machine-uuid-1
    life_id: 0
    instance_id: source-instance-1
    display_name: source-display-1
`

// controllerModelDumpHAWorkloadYAML extends the HA dump with a workload
// machine "5" hosted in the controller model.
const controllerModelDumpHAWorkloadYAML = `version: 4.1.0
payload:
  net_node:
  - uuid: nn-0
  - uuid: nn-1
  - uuid: nn-w
  link_layer_device:
  - uuid: lld-0
    net_node_uuid: nn-1
    name: eth0
    device_type_id: 2
    virtual_port_type_id: 0
  ip_address:
  - uuid: ip-uuid-0
    net_node_uuid: nn-1
    device_uuid: lld-0
    address_value: 10.0.0.5/24
    type_id: 0
    config_type_id: 0
    origin_id: 0
    scope_id: 0
  charm:
  - uuid: charm-uuid-0
    reference_name: controller
    revision: 1
    source_id: 1
    architecture_id: 0
  application:
  - uuid: app-uuid-0
    name: controller
    life_id: 0
    charm_uuid: charm-uuid-0
    space_uuid: 656b4a82-e28c-53d6-a014-f0dd53417eb6
  application_controller:
  - application_uuid: app-uuid-0
  unit:
  - uuid: unit-uuid-0
    name: controller/0
    life_id: 1
    application_uuid: app-uuid-0
    net_node_uuid: nn-0
    charm_uuid: charm-uuid-0
  - uuid: unit-uuid-1
    name: controller/1
    life_id: 0
    application_uuid: app-uuid-0
    net_node_uuid: nn-1
    charm_uuid: charm-uuid-0
  machine:
  - uuid: machine-uuid-0
    name: "0"
    net_node_uuid: nn-0
    life_id: 1
  - uuid: machine-uuid-1
    name: "1"
    net_node_uuid: nn-1
    life_id: 0
  - uuid: machine-uuid-w
    name: "5"
    net_node_uuid: nn-w
    life_id: 0
  machine_cloud_instance:
  - machine_uuid: machine-uuid-0
    life_id: 0
    instance_id: source-instance-0
    display_name: source-display-0
  - machine_uuid: machine-uuid-1
    life_id: 0
    instance_id: source-instance-1
    display_name: source-display-1
  - machine_uuid: machine-uuid-w
    life_id: 0
    instance_id: workload-instance
    display_name: workload-display
`

func (s *loadSuite) TestLoadStageEndToEnd(c *tc.C) {
	stagingDir := c.MkDir()
	c.Setenv("TMPDIR", stagingDir)
	files := map[string][]byte{
		"juju-backup/metadata.json": []byte(metadataJSON("4.1.0")),
		"juju-backup/dump/controller.yaml": []byte(controllerDump(
			modelRow(testControllerModelUUID, "controller", "cloud-lxd"),
			modelRow(testModelAUUID, "workload-a", "cloud-lxd"),
			modelRow(testModelBUUID, "workload-b", "cloud-lxd"),
		)),
		"juju-backup/dump/models/" + testControllerModelUUID + ".yaml": []byte(controllerModelDumpYAML),
		"juju-backup/dump/models/" + testModelAUUID + ".yaml":          []byte(emptyModelDumpYAML),
		"juju-backup/dump/models/" + testModelBUUID + ".yaml":          []byte(emptyModelDumpYAML),
		"juju-backup/root.tar": rootTar(c),
	}
	archivePath, sum := writeArchive(c, files)

	openModelDB := func(_ context.Context, modelUUID string) (*sql.DB, error) {
		return s.openModelDB(c, modelUUID), nil
	}
	dataDir := c.MkDir()

	summary, err := recovery.Load(c.Context(), recovery.LoadParams{
		ControllerDB:        s.DB(),
		OpenModelDB:         openModelDB,
		ArchivePath:         archivePath,
		ExpectedSHA256:      sum,
		BundleDir:           c.MkDir(),
		DataDir:             dataDir,
		ControllerModelUUID: testControllerModelUUID,
		MachinePatch: &domainrecovery.MachinePatch{
			MachineName: "0",
			InstanceID:  "replacement-instance",
			DisplayName: "replacement-display",
		},
		Logger: loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)

	entries, err := os.ReadDir(stagingDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(entries, tc.HasLen, 0)

	c.Check(summary.ControllerUUID, tc.Equals, testControllerUUID)
	c.Check(summary.ControllerName, tc.Equals, "source-ctrl")
	c.Check(summary.SourceAgentVersion, tc.Equals, "4.1.0")
	c.Check(summary.Models, tc.Equals, 3)
	c.Check(summary.HANodes, tc.Equals, int64(3))
	c.Check(summary.DeadControllerMachines, tc.HasLen, 0)
	c.Check(summary.MachinesNotAlive, tc.Equals, 0)

	// The controller machine was patched onto the replacement instance.
	modelDB, err := openModelDB(c.Context(), testControllerModelUUID)
	c.Assert(err, tc.ErrorIsNil)
	defer modelDB.Close()
	var instanceID string
	err = modelDB.QueryRow(
		"SELECT instance_id FROM machine_cloud_instance WHERE machine_uuid = ?",
		"machine-uuid-0").Scan(&instanceID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(instanceID, tc.Equals, "replacement-instance")

	// The controller row carries the source identity.
	var controllerUUID string
	err = s.DB().QueryRow("SELECT uuid FROM controller").Scan(&controllerUUID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(controllerUUID, tc.Equals, testControllerUUID)

	// The file-backed object store pin is written for the charm.
	pin, err := os.ReadFile(filepath.Join(dataDir, "recovery", "file-backed-object-store"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(len(pin) > 0, tc.IsTrue)

	// A machine recovery has no controller charm: no controller.conf.
	_, err = os.Stat(filepath.Join(
		dataDir, "agents", "controller-0", "controller.conf"))
	c.Check(err, tc.ErrorIs, os.ErrNotExist)
}

// TestLoadStageK8sControllerConf checks DNS observations and the controller
// charm config file that its archived install hook will not recreate.
func (s *loadSuite) TestLoadStageK8sControllerConf(c *tc.C) {
	files := validFiles()
	files["juju-backup/dump/controller.yaml"] = []byte(strings.ReplaceAll(controllerDump(
		modelRow(testControllerModelUUID, "controller", "cloud-k8s"),
		modelRow(testModelAUUID, "workload-a", "cloud-k8s"),
		modelRow(testModelBUUID, "workload-b", "cloud-k8s"),
	), "model_type_id: 0", "model_type_id: 1"))
	files["juju-backup/dump/models/"+testControllerModelUUID+".yaml"] = []byte(`version: 4.1.0
payload:
  net_node:
  - uuid: nn-0
  fqdn_address:
  - uuid: source-dns
    address: source.example.com
    scope_id: 1
  net_node_fqdn_address:
  - net_node_uuid: nn-0
    address_uuid: source-dns
  charm:
  - uuid: charm-uuid-0
    reference_name: controller
    revision: 1
    source_id: 1
    architecture_id: 0
  application:
  - uuid: app-uuid-0
    name: controller
    life_id: 0
    charm_uuid: charm-uuid-0
    space_uuid: 656b4a82-e28c-53d6-a014-f0dd53417eb6
  application_controller:
  - application_uuid: app-uuid-0
  unit:
  - uuid: unit-uuid-0
    name: controller/0
    life_id: 0
    application_uuid: app-uuid-0
    net_node_uuid: nn-0
    charm_uuid: charm-uuid-0
`)
	files["juju-backup/dump/models/"+testModelAUUID+".yaml"] = []byte(emptyModelDumpYAML)
	files["juju-backup/dump/models/"+testModelBUUID+".yaml"] = []byte(emptyModelDumpYAML)
	files["juju-backup/root.tar"] = rootTar(c)
	archivePath, sum := writeArchive(c, files)

	dataDir := c.MkDir()
	_, err := recovery.Load(c.Context(), recovery.LoadParams{
		ControllerDB: s.DB(),
		OpenModelDB: func(_ context.Context, modelUUID string) (*sql.DB, error) {
			return s.openModelDB(c, modelUUID), nil
		},
		ArchivePath:             archivePath,
		ExpectedSHA256:          sum,
		BundleDir:               c.MkDir(),
		DataDir:                 dataDir,
		ControllerModelUUID:     testControllerModelUUID,
		ControllerUnitAddresses: []string{"replacement.example.com"},
		Logger:                  loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)

	charmConf, err := os.Stat(filepath.Join(
		dataDir, "agents", "controller-0", "controller.conf"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(charmConf.Size(), tc.Equals, int64(0))
	db := s.openModelDB(c, testControllerModelUUID)
	defer db.Close()
	var address string
	err = db.QueryRowContext(c.Context(), `
SELECT fa.address FROM fqdn_address AS fa
JOIN net_node_fqdn_address AS nnfa ON nnfa.address_uuid = fa.uuid
WHERE nnfa.net_node_uuid = 'nn-0'`).Scan(&address)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(address, tc.Equals, "replacement.example.com")
}

// TestLoadStageHAMachineSelection checks that the replacement patches the
// archived live controller machine, not an assumed ordinal: machine "0"
// is dead in this dump, machine "1" hosts the live controller unit.
func (s *loadSuite) TestLoadStageHAMachineSelection(c *tc.C) {
	files := map[string][]byte{
		"juju-backup/metadata.json": []byte(metadataJSON("4.1.0")),
		"juju-backup/dump/controller.yaml": []byte(controllerDump(
			modelRow(testControllerModelUUID, "controller", "cloud-lxd"),
			modelRow(testModelAUUID, "workload-a", "cloud-lxd"),
			modelRow(testModelBUUID, "workload-b", "cloud-lxd"),
		)),
		"juju-backup/dump/models/" + testControllerModelUUID + ".yaml": []byte(controllerModelDumpHAYAML),
		"juju-backup/dump/models/" + testModelAUUID + ".yaml":          []byte(emptyModelDumpYAML),
		"juju-backup/dump/models/" + testModelBUUID + ".yaml":          []byte(emptyModelDumpYAML),
		"juju-backup/root.tar": rootTar(c),
	}
	archivePath, sum := writeArchive(c, files)

	dataDir := c.MkDir()
	openModelDB := func(_ context.Context, modelUUID string) (*sql.DB, error) {
		return s.openModelDB(c, modelUUID), nil
	}
	summary, err := recovery.Load(c.Context(), recovery.LoadParams{
		ControllerDB:        s.DB(),
		OpenModelDB:         openModelDB,
		ArchivePath:         archivePath,
		ExpectedSHA256:      sum,
		BundleDir:           c.MkDir(),
		DataDir:             dataDir,
		ControllerModelUUID: testControllerModelUUID,
		MachinePatch: &domainrecovery.MachinePatch{
			InstanceID: "replacement-instance",
		},
		Logger: loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)

	// The replacement's instance id was patched onto machine "1" (the
	// live controller machine); machine "0" keeps its archived record.
	modelDB, err := openModelDB(c.Context(), testControllerModelUUID)
	c.Assert(err, tc.ErrorIsNil)
	defer modelDB.Close()
	var instanceID string
	err = modelDB.QueryRow(
		"SELECT instance_id FROM machine_cloud_instance WHERE machine_uuid = ?",
		"machine-uuid-1").Scan(&instanceID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(instanceID, tc.Equals, "replacement-instance")

	err = modelDB.QueryRow(
		"SELECT instance_id FROM machine_cloud_instance WHERE machine_uuid = ?",
		"machine-uuid-0").Scan(&instanceID)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(instanceID, tc.Equals, "source-instance-0")

	// Machine "0" is a dead controller machine awaiting removal.
	c.Check(summary.DeadControllerMachines, tc.DeepEquals, []string{"0"})
}

// TestLoadStageHAWorkloadMachineNotDead checks that workload machines
// hosted in the controller model are not reported as dead controller
// machines: only machines hosting the controller application are.
func (s *loadSuite) TestLoadStageHAWorkloadMachineNotDead(c *tc.C) {
	// HA dump plus a workload machine "5" in the controller model: the
	// dead controller machine is "0" only — "1" is the patch target and
	// "5" hosts no controller unit.
	workloadYAML := controllerModelDumpHAWorkloadYAML
	files := map[string][]byte{
		"juju-backup/metadata.json": []byte(metadataJSON("4.1.0")),
		"juju-backup/dump/controller.yaml": []byte(controllerDump(
			modelRow(testControllerModelUUID, "controller", "cloud-lxd"),
			modelRow(testModelAUUID, "workload-a", "cloud-lxd"),
			modelRow(testModelBUUID, "workload-b", "cloud-lxd"),
		)),
		"juju-backup/dump/models/" + testControllerModelUUID + ".yaml": []byte(workloadYAML),
		"juju-backup/dump/models/" + testModelAUUID + ".yaml":          []byte(emptyModelDumpYAML),
		"juju-backup/dump/models/" + testModelBUUID + ".yaml":          []byte(emptyModelDumpYAML),
		"juju-backup/root.tar": rootTar(c),
	}
	archivePath, sum := writeArchive(c, files)

	summary, err := recovery.Load(c.Context(), recovery.LoadParams{
		ControllerDB: s.DB(),
		OpenModelDB: func(_ context.Context, modelUUID string) (*sql.DB, error) {
			return s.openModelDB(c, modelUUID), nil
		},
		ArchivePath:         archivePath,
		ExpectedSHA256:      sum,
		BundleDir:           c.MkDir(),
		DataDir:             c.MkDir(),
		ControllerModelUUID: testControllerModelUUID,
		MachinePatch: &domainrecovery.MachinePatch{
			InstanceID: "replacement-instance",
		},
		Logger: loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)

	// The dead controller machine "0" is listed; the patch target "1"
	// and the workload machine "5" are not.
	c.Check(summary.DeadControllerMachines, tc.DeepEquals, []string{"0"})
}

func (s *loadSuite) TestLoadStageChecksumMismatch(c *tc.C) {
	files := validFiles()
	files["juju-backup/root.tar"] = rootTar(c)
	archivePath, _ := writeArchive(c, files)

	_, err := recovery.Load(c.Context(), recovery.LoadParams{
		ControllerDB:        s.DB(),
		ArchivePath:         archivePath,
		ExpectedSHA256:      "deadbeef",
		BundleDir:           c.MkDir(),
		DataDir:             c.MkDir(),
		ControllerModelUUID: testControllerModelUUID,
		Logger:              loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorMatches, "archive checksum mismatch.*")

	// Nothing was loaded: the controller table is still empty.
	var count int
	c.Assert(s.DB().QueryRow("SELECT COUNT(*) FROM controller").Scan(&count), tc.ErrorIsNil)
	c.Check(count, tc.Equals, 0)
}

func (s *loadSuite) TestLoadStageCleansUpStagingOnFailure(c *tc.C) {
	files := validFiles()
	files["juju-backup/root.tar"] = rootTar(c)
	archivePath, sum := writeArchive(c, files)
	stagingDir := c.MkDir()
	c.Setenv("TMPDIR", stagingDir)
	openErr := errors.New("cannot open model database")

	_, err := recovery.Load(c.Context(), recovery.LoadParams{
		ControllerDB: s.DB(),
		OpenModelDB: func(context.Context, string) (*sql.DB, error) {
			return nil, openErr
		},
		ArchivePath:         archivePath,
		ExpectedSHA256:      sum,
		ControllerModelUUID: testControllerModelUUID,
		Logger:              loggertesting.WrapCheckLog(c),
	})
	c.Check(err, tc.ErrorIs, openErr)
	entries, err := os.ReadDir(stagingDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(entries, tc.HasLen, 0)
}
