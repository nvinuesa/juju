// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/juju/tc"

	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/internal/restore"
)

type validateSuite struct{}

const (
	testControllerUUID      = "c0ffee00-1111-2222-3333-444455556666"
	testControllerModelUUID = "c0ffee00-aaaa-2222-3333-444455556666"
	testModelAUUID          = "c0ffee00-bbbb-2222-3333-444455556666"
	testModelBUUID          = "c0ffee00-cccc-2222-3333-444455556666"
)

func metadataJSON(agentVersion string) string {
	return `{` +
		`"ID":"20260930-120000.aabb-ccdd-eeff",` +
		`"FormatVersion":2,` +
		`"Checksum":"abc",` +
		`"ChecksumFormat":"SHA-256, hex encoded",` +
		`"Size":10,` +
		`"Stored":"0001-01-01T00:00:00Z",` +
		`"Started":"2026-09-30T12:00:00Z",` +
		`"Finished":"2026-09-30T12:00:34Z",` +
		`"Notes":"",` +
		`"ModelUUID":"` + testControllerModelUUID + `",` +
		`"Machine":"0",` +
		`"Hostname":"myhost",` +
		`"Version":"` + agentVersion + `",` +
		`"ControllerUUID":"` + testControllerUUID + `",` +
		`"HANodes":3,` +
		`"ControllerMachineID":"0",` +
		`"ControllerMachineInstanceID":"inst-10101010"` +
		`}` + "\n"
}

// controllerDump renders a minimal controller database dump with the
// given per-model cloud type IDs and model type IDs. Cloud type 1 is
// "lxd", 2 is "kubernetes"; model type 0 is "iaas", 1 is "caas".
func controllerDump(models ...[3]string) string {
	out := "payload:\n" +
		"  controller:\n" +
		"  - uuid: " + testControllerUUID + "\n" +
		"    model_uuid: " + testControllerModelUUID + "\n" +
		"    target_version: 4.1.0\n" +
		"    api_port: \"17070\"\n" +
		"  cloud:\n" +
		"  - uuid: cloud-lxd\n    name: lxd\n    cloud_type_id: 1\n    endpoint: ''\n    skip_tls_verify: false\n" +
		"  - uuid: cloud-k8s\n    name: myk8s\n    cloud_type_id: 2\n    endpoint: ''\n    skip_tls_verify: false\n" +
		"  cloud_type:\n" +
		"  - id: 1\n    type: lxd\n" +
		"  - id: 2\n    type: kubernetes\n" +
		"  model_type:\n" +
		"  - id: 0\n    type: iaas\n" +
		"  - id: 1\n    type: caas\n" +
		"  controller_config:\n" +
		"  - key: controller-name\n    value: source-ctrl\n" +
		"  model:\n"
	for _, m := range models {
		out += fmt.Sprintf(
			"  - uuid: %s\n    name: %s\n    cloud_uuid: %s\n    model_type_id: %s\n"+
				"    activated: true\n    life_id: 0\n    qualifier: ''\n",
			m[0], m[1], m[2], "0")
	}
	return out
}

// modelRow is a helper building controllerDump model entries:
// uuid, name, cloud uuid. Model type is always iaas (0); use
// controllerDump directly for caas models.
func modelRow(uuid, name, cloudUUID string) [3]string {
	return [3]string{uuid, name, cloudUUID}
}

func makeArchive(c *tc.C, files map[string][]byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := files[name]
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o600,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}
		c.Assert(tw.WriteHeader(hdr), tc.ErrorIsNil)
		_, err := tw.Write(data)
		c.Assert(err, tc.ErrorIsNil)
	}
	c.Assert(tw.Close(), tc.ErrorIsNil)
	c.Assert(gz.Close(), tc.ErrorIsNil)
	return buf.Bytes()
}

func writeArchive(c *tc.C, files map[string][]byte) (string, string) {
	archive := makeArchive(c, files)
	sum := sha256.Sum256(archive)
	path := filepath.Join(c.MkDir(), "juju-backup.tar.gz")
	c.Assert(os.WriteFile(path, archive, 0o600), tc.ErrorIsNil)
	return path, hex.EncodeToString(sum[:])
}

func validFiles() map[string][]byte {
	return map[string][]byte{
		"juju-backup/metadata.json": []byte(metadataJSON("4.1.0")),
		"juju-backup/dump/controller.yaml": []byte(controllerDump(
			modelRow(testControllerModelUUID, "controller", "cloud-lxd"),
			modelRow(testModelAUUID, "workload-a", "cloud-lxd"),
			modelRow(testModelBUUID, "workload-b", "cloud-lxd"),
		)),
		"juju-backup/dump/models/" + testControllerModelUUID + ".yaml": []byte("payload: {}\n"),
		"juju-backup/dump/models/" + testModelAUUID + ".yaml":          []byte("payload: {}\n"),
		"juju-backup/dump/models/" + testModelBUUID + ".yaml":          []byte("payload: {}\n"),
		"juju-backup/root.tar": []byte("blobs"),
	}
}

func (s *validateSuite) TestValidateArchive(c *tc.C) {
	path, sum := writeArchive(c, validFiles())

	info, err := restore.ValidateArchive(c.Context(), path, sum)
	c.Assert(err, tc.ErrorIsNil)

	c.Check(info.AgentVersion, tc.Equals, semversion.MustParse("4.1.0"))
	c.Check(info.ControllerUUID, tc.Equals, testControllerUUID)
	c.Check(info.ControllerName, tc.Equals, "source-ctrl")
	c.Check(info.ControllerModelUUID, tc.Equals, testControllerModelUUID)
	c.Check(info.HANodes, tc.Equals, int64(3))
	c.Check(info.CloudName, tc.Equals, "lxd")
	c.Check(info.CloudType, tc.Equals, "lxd")
	c.Check(info.Checksum, tc.Equals, sum)
	c.Check(info.Size > 0, tc.IsTrue)

	c.Assert(info.Models, tc.HasLen, 3)
	c.Check(info.Models[0].UUID, tc.Equals, testControllerModelUUID)
	c.Check(info.Models[0].ModelType, tc.Equals, "iaas")
	c.Check(info.Models[1].CloudType, tc.Equals, "lxd")
}

func (s *validateSuite) TestValidateArchiveChecksumMismatch(c *tc.C) {
	path, _ := writeArchive(c, validFiles())

	_, err := restore.ValidateArchive(c.Context(), path, "deadbeef")
	c.Assert(err, tc.ErrorMatches, "archive checksum mismatch: expected sha256 .deadbeef., archive is .*")
}

func (s *validateSuite) TestValidateArchiveMissingMetadata(c *tc.C) {
	files := validFiles()
	delete(files, "juju-backup/metadata.json")
	path, sum := writeArchive(c, files)

	_, err := restore.ValidateArchive(c.Context(), path, sum)
	c.Assert(err, tc.ErrorMatches, "archive is missing juju-backup/metadata.json")
}

func (s *validateSuite) TestValidateArchiveMissingControllerDump(c *tc.C) {
	files := validFiles()
	delete(files, "juju-backup/dump/controller.yaml")
	path, sum := writeArchive(c, files)

	_, err := restore.ValidateArchive(c.Context(), path, sum)
	c.Assert(err, tc.ErrorMatches, "archive is missing juju-backup/dump/controller.yaml")
}

func (s *validateSuite) TestValidateArchiveMissingModelDump(c *tc.C) {
	files := validFiles()
	delete(files, "juju-backup/dump/models/"+testModelAUUID+".yaml")
	path, sum := writeArchive(c, files)

	_, err := restore.ValidateArchive(c.Context(), path, sum)
	c.Assert(err, tc.ErrorMatches,
		"archive is missing the database dump for model .workload-a. .*")
}

func (s *validateSuite) TestValidateArchiveUnknownModelDump(c *tc.C) {
	files := validFiles()
	files["juju-backup/dump/models/00000000-0000-0000-0000-000000000000.yaml"] = []byte("payload: {}\n")
	path, sum := writeArchive(c, files)

	_, err := restore.ValidateArchive(c.Context(), path, sum)
	c.Assert(err, tc.ErrorMatches,
		"archive contains a database dump for unknown model .*")
}

func (s *validateSuite) TestValidateArchiveRejectsTraversal(c *tc.C) {
	files := validFiles()
	files["juju-backup/../escape"] = []byte("x")
	path, _ := writeArchive(c, files)

	_, err := restore.ValidateArchive(c.Context(), path, "")
	c.Assert(err, tc.ErrorMatches, "archive contains unsafe path .*")
}

func (s *validateSuite) TestValidateArchiveRejectsAbsolutePath(c *tc.C) {
	files := validFiles()
	files["/abs/path"] = []byte("x")
	path, _ := writeArchive(c, files)

	_, err := restore.ValidateArchive(c.Context(), path, "")
	c.Assert(err, tc.ErrorMatches, "archive contains unsafe path .*")
}

func (s *validateSuite) TestValidateArchiveRejectsDuplicate(c *tc.C) {
	// The second entry cleans to the same path as metadata.json.
	files := validFiles()
	files["juju-backup/./metadata.json"] = []byte(metadataJSON("4.1.0"))
	path, _ := writeArchive(c, files)

	_, err := restore.ValidateArchive(c.Context(), path, "")
	c.Assert(err, tc.ErrorMatches, "archive contains duplicate path .*")
}

func (s *validateSuite) TestValidateArchiveRejectsNonRegular(c *tc.C) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{
		"juju-backup/metadata.json",
		"juju-backup/dump/controller.yaml",
	} {
		var data []byte
		if name == "juju-backup/metadata.json" {
			data = []byte(metadataJSON("4.1.0"))
		} else {
			data = []byte(controllerDump(modelRow(testControllerModelUUID, "controller", "cloud-lxd")))
		}
		c.Assert(tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg,
		}), tc.ErrorIsNil)
		_, err := tw.Write(data)
		c.Assert(err, tc.ErrorIsNil)
	}
	c.Assert(tw.WriteHeader(&tar.Header{
		Name: "juju-backup/evil", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd",
	}), tc.ErrorIsNil)
	c.Assert(tw.Close(), tc.ErrorIsNil)
	c.Assert(gz.Close(), tc.ErrorIsNil)

	path := filepath.Join(c.MkDir(), "juju-backup.tar.gz")
	c.Assert(os.WriteFile(path, buf.Bytes(), 0o600), tc.ErrorIsNil)

	_, err := restore.ValidateArchive(c.Context(), path, "")
	c.Assert(err, tc.ErrorMatches, "archive entry .* is not a regular file")
}

func (s *validateSuite) TestCheckAgentVersion(c *tc.C) {
	info := &restore.ArchiveInfo{AgentVersion: semversion.MustParse("4.1.0")}
	c.Check(info.CheckAgentVersion(semversion.MustParse("4.1.0")), tc.ErrorIsNil)

	// The official build number is packaging, not a schema difference:
	// an archive from a released 4.1-beta3.1 restores onto a dev
	// 4.1-beta3 binary.
	info.AgentVersion = semversion.MustParse("4.1-beta3.1")
	c.Check(info.CheckAgentVersion(semversion.MustParse("4.1-beta3")), tc.ErrorIsNil)
	info.AgentVersion = semversion.MustParse("4.1.0")
	c.Check(info.CheckAgentVersion(semversion.MustParse("4.1.0.1")), tc.ErrorIsNil)

	err := info.CheckAgentVersion(semversion.MustParse("4.1.1"))
	c.Assert(err, tc.ErrorMatches,
		"archive was created by agent version 4.1.0 but this binary is 4.1.1.*")
	err = info.CheckAgentVersion(semversion.MustParse("4.1-beta1"))
	c.Assert(err, tc.ErrorMatches,
		"archive was created by agent version 4.1.0 but this binary is 4.1-beta1.*")
}

func (s *validateSuite) TestModelFamily(c *tc.C) {
	info := &restore.ArchiveInfo{Models: []restore.ModelInfo{
		{UUID: "a", Name: "controller", ModelType: "iaas"},
		{UUID: "b", Name: "workload", ModelType: "iaas"},
	}}
	family, err := info.ModelFamily()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(family, tc.Equals, "iaas")

	info.Models[1].ModelType = "caas"
	_, err = info.ModelFamily()
	c.Assert(err, tc.ErrorMatches,
		"source controller has mixed model types: model .workload. is caas, expected iaas")
}

func (s *validateSuite) TestCheckProviderFamily(c *tc.C) {
	info := &restore.ArchiveInfo{CloudType: "lxd"}
	c.Check(info.CheckProviderFamily("lxd"), tc.ErrorIsNil)

	err := info.CheckProviderFamily("ec2")
	c.Assert(err, tc.ErrorMatches,
		"archive comes from a .lxd. controller but the bootstrap cloud is .ec2.*")
}
