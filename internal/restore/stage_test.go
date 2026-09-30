// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore_test

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"

	"github.com/juju/tc"

	loggertesting "github.com/juju/juju/internal/logger/testing"
	"github.com/juju/juju/internal/restore"
)

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

func rootTar(c *tc.C) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("blob content")
	c.Assert(tw.WriteHeader(&tar.Header{
		Name: "var/lib/juju/objectstore/some-ns/somehash", Mode: 0o600,
		Size: int64(len(content)), Typeflag: tar.TypeReg,
	}), tc.ErrorIsNil)
	_, err := tw.Write(content)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(tw.Close(), tc.ErrorIsNil)
	return buf.Bytes()
}

func (s *loadSuite) TestLoadStageEndToEnd(c *tc.C) {
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

	summary, err := restore.Load(c.Context(), restore.LoadParams{
		ControllerDB:        s.DB(),
		OpenModelDB:         openModelDB,
		ArchivePath:         archivePath,
		ExpectedSHA256:      sum,
		BundleDir:           c.MkDir(),
		DataDir:             dataDir,
		ControllerModelUUID: testControllerModelUUID,
		MachinePatch: &restore.MachinePatch{
			MachineName: "0",
			InstanceID:  "replacement-instance",
			DisplayName: "replacement-display",
		},
		Logger: loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)

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
	pin, err := os.ReadFile(filepath.Join(dataDir, "restore", "file-backed-object-store"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(len(pin) > 0, tc.IsTrue)

	// A machine restore has no controller charm: no controller.conf.
	_, err = os.Stat(filepath.Join(
		dataDir, "agents", "controller-0", "controller.conf"))
	c.Check(err, tc.ErrorIs, os.ErrNotExist)
}

// TestLoadStageCAASControllerConf checks that a Kubernetes restore (no
// machine patch) creates the empty controller.conf the charm's install
// hook would have created.
func (s *loadSuite) TestLoadStageCAASControllerConf(c *tc.C) {
	files := validFiles()
	files["juju-backup/dump/models/"+testControllerModelUUID+".yaml"] = []byte(controllerModelDumpYAML)
	files["juju-backup/dump/models/"+testModelAUUID+".yaml"] = []byte(emptyModelDumpYAML)
	files["juju-backup/dump/models/"+testModelBUUID+".yaml"] = []byte(emptyModelDumpYAML)
	files["juju-backup/root.tar"] = rootTar(c)
	archivePath, sum := writeArchive(c, files)

	dataDir := c.MkDir()
	_, err := restore.Load(c.Context(), restore.LoadParams{
		ControllerDB: s.DB(),
		OpenModelDB: func(_ context.Context, modelUUID string) (*sql.DB, error) {
			return s.openModelDB(c, modelUUID), nil
		},
		ArchivePath:             archivePath,
		ExpectedSHA256:          sum,
		BundleDir:               c.MkDir(),
		DataDir:                 dataDir,
		ControllerModelUUID:     testControllerModelUUID,
		ControllerUnitAddresses: []string{"10.1.0.1:17070"},
		Logger:                  loggertesting.WrapCheckLog(c),
	})
	c.Assert(err, tc.ErrorIsNil)

	charmConf, err := os.Stat(filepath.Join(
		dataDir, "agents", "controller-0", "controller.conf"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(charmConf.Size(), tc.Equals, int64(0))
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
	summary, err := restore.Load(c.Context(), restore.LoadParams{
		ControllerDB:        s.DB(),
		OpenModelDB:         openModelDB,
		ArchivePath:         archivePath,
		ExpectedSHA256:      sum,
		BundleDir:           c.MkDir(),
		DataDir:             dataDir,
		ControllerModelUUID: testControllerModelUUID,
		MachinePatch: &restore.MachinePatch{
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

	summary, err := restore.Load(c.Context(), restore.LoadParams{
		ControllerDB: s.DB(),
		OpenModelDB: func(_ context.Context, modelUUID string) (*sql.DB, error) {
			return s.openModelDB(c, modelUUID), nil
		},
		ArchivePath:         archivePath,
		ExpectedSHA256:      sum,
		BundleDir:           c.MkDir(),
		DataDir:             c.MkDir(),
		ControllerModelUUID: testControllerModelUUID,
		MachinePatch: &restore.MachinePatch{
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

	_, err := restore.Load(c.Context(), restore.LoadParams{
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

func (s *loadSuite) TestUnpackObjectsBundle(c *tc.C) {
	files := validFiles()
	files["juju-backup/root.tar"] = rootTar(c)
	archivePath, _ := writeArchive(c, files)

	dest := c.MkDir()
	err := restore.UnpackObjectsBundle(c.Context(), archivePath, dest)
	c.Assert(err, tc.ErrorIsNil)

	content, err := os.ReadFile(filepath.Join(dest, "var", "lib", "juju", "objectstore", "some-ns", "somehash"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(content), tc.Equals, "blob content")
}

func (s *loadSuite) TestUnpackObjectsBundleRejectsTraversal(c *tc.C) {
	var inner bytes.Buffer
	tw := tar.NewWriter(&inner)
	content := []byte("x")
	c.Assert(tw.WriteHeader(&tar.Header{
		Name: "../escape", Mode: 0o600, Size: 1, Typeflag: tar.TypeReg,
	}), tc.ErrorIsNil)
	_, err := tw.Write(content)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(tw.Close(), tc.ErrorIsNil)

	files := validFiles()
	files["juju-backup/root.tar"] = inner.Bytes()
	archivePath, _ := writeArchive(c, files)

	err = restore.UnpackObjectsBundle(c.Context(), archivePath, c.MkDir())
	c.Assert(err, tc.ErrorMatches, "object bundle contains unsafe path .*")
}

func (s *loadSuite) TestUnpackObjectsBundleSkipsSymlinks(c *tc.C) {
	// k8s controllers archive their tools binaries as symlinks; the
	// bundle unpacker skips them (restore installs only referenced
	// object-store blobs) instead of failing. The symlink comes FIRST
	// here: skipping must never stop the rest of the bundle from being
	// unpacked.
	var inner bytes.Buffer
	tw := tar.NewWriter(&inner)
	c.Assert(tw.WriteHeader(&tar.Header{
		Name:     "var/lib/juju/tools/controller-0/jujuagentd",
		Typeflag: tar.TypeSymlink, Linkname: "/charm/bin/containeragent",
	}), tc.ErrorIsNil)
	content := []byte("blob content")
	c.Assert(tw.WriteHeader(&tar.Header{
		Name: "var/lib/juju/objectstore/ns/somehash", Mode: 0o600,
		Size: int64(len(content)), Typeflag: tar.TypeReg,
	}), tc.ErrorIsNil)
	_, err := tw.Write(content)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(tw.Close(), tc.ErrorIsNil)

	files := validFiles()
	files["juju-backup/root.tar"] = inner.Bytes()
	archivePath, _ := writeArchive(c, files)

	dest := c.MkDir()
	err = restore.UnpackObjectsBundle(c.Context(), archivePath, dest)
	c.Assert(err, tc.ErrorIsNil)

	got, err := os.ReadFile(filepath.Join(dest, "var", "lib", "juju", "objectstore", "ns", "somehash"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(got), tc.Equals, "blob content")
	if _, err := os.Lstat(filepath.Join(dest, "var", "lib", "juju", "tools")); err == nil {
		c.Errorf("symlink entry was extracted")
	}
}

func (s *loadSuite) TestUnpackObjectsBundleAcceptsTypeRegA(c *tc.C) {
	// Archives written by tars that do not set the typeflag carry
	// regular files as TypeRegA; the bundle must still be found.
	var inner bytes.Buffer
	tw := tar.NewWriter(&inner)
	content := []byte("old-tar blob")
	c.Assert(tw.WriteHeader(&tar.Header{
		Name: "var/lib/juju/objectstore/ns/oldhash", Mode: 0o600,
		Size: int64(len(content)), Typeflag: tar.TypeRegA,
	}), tc.ErrorIsNil)
	_, err := tw.Write(content)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(tw.Close(), tc.ErrorIsNil)

	files := validFiles()
	files["juju-backup/root.tar"] = inner.Bytes()
	archivePath, _ := writeArchive(c, files)

	dest := c.MkDir()
	err = restore.UnpackObjectsBundle(c.Context(), archivePath, dest)
	c.Assert(err, tc.ErrorIsNil)

	got, err := os.ReadFile(filepath.Join(dest, "var", "lib", "juju", "objectstore", "ns", "oldhash"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(got), tc.Equals, "old-tar blob")
}
