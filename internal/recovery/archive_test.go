// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/juju/tc"

	"github.com/juju/juju/internal/recovery"
)

func (s *validateSuite) TestReadArchiveStagesModelDumps(c *tc.C) {
	files := validFiles()
	archivePath, sum := writeArchive(c, withManifest(c, files))
	stagingDir := c.MkDir()
	c.Setenv("TMPDIR", stagingDir)

	contents, info, err := recovery.ReadArchive(c.Context(), archivePath, sum)
	c.Assert(err, tc.ErrorIsNil)
	c.Cleanup(func() { c.Check(contents.Close(), tc.ErrorIsNil) })
	c.Check(info.Checksum, tc.Equals, sum)
	c.Check(contents.ControllerDump, tc.DeepEquals, files["juju-backup/dump/controller.yaml"])
	c.Check(contents.ModelDumps, tc.HasLen, 3)
	for modelUUID, filename := range contents.ModelDumps {
		c.Check(filepath.Dir(filepath.Dir(filename)), tc.Equals, stagingDir)
		data, err := os.ReadFile(filename)
		c.Assert(err, tc.ErrorIsNil)
		c.Check(data, tc.DeepEquals, files["juju-backup/dump/models/"+modelUUID+".yaml"])
		stat, err := os.Stat(filename)
		c.Assert(err, tc.ErrorIsNil)
		c.Check(stat.Mode().Perm(), tc.Equals, os.FileMode(0600))
	}

	c.Assert(contents.Close(), tc.ErrorIsNil)
	entries, err := os.ReadDir(stagingDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(entries, tc.HasLen, 0)
}

func (s *validateSuite) TestValidateArchiveCleansUpStaging(c *tc.C) {
	invalidMetadata := validFiles()
	invalidMetadata["juju-backup/metadata.json"] = []byte("invalid json")
	missingModel := validFiles()
	delete(missingModel, "juju-backup/dump/models/"+testModelAUUID+".yaml")
	invalidModel := caasFiles("payload: [invalid yaml")
	invalidManifest := withManifest(c, validFiles())
	invalidManifest["juju-backup/dump/models/"+testModelAUUID+".yaml"] = []byte("tampered")
	for _, test := range []struct {
		name     string
		files    map[string][]byte
		checksum string
		error    string
		cancel   bool
	}{
		{name: "success", files: validFiles()},
		{name: "checksum", files: validFiles(), checksum: "deadbeef", error: "archive checksum mismatch:.*"},
		{name: "metadata", files: invalidMetadata, error: "parsing juju-backup/metadata.json:.*"},
		{name: "model inventory", files: missingModel, error: "archive is missing the database dump.*"},
		{name: "model decode", files: invalidModel, error: "model .*: decoding dump:.*"},
		{name: "manifest", files: invalidManifest, error: "juju-backup/manifest.json records.*"},
		{name: "cancelled", files: validFiles(), cancel: true},
	} {
		c.Logf("%s", test.name)
		archivePath, sum := writeArchive(c, test.files)
		stagingDir := c.MkDir()
		c.Setenv("TMPDIR", stagingDir)
		if test.checksum != "" {
			sum = test.checksum
		}
		ctx, cancel := context.WithCancel(c.Context())
		if test.cancel {
			cancel()
		}
		_, err := recovery.ValidateArchive(ctx, archivePath, sum)
		cancel()
		if test.cancel {
			c.Check(err, tc.ErrorIs, context.Canceled)
		} else if test.error == "" {
			c.Assert(err, tc.ErrorIsNil)
		} else {
			c.Check(err, tc.ErrorMatches, test.error)
		}
		entries, err := os.ReadDir(stagingDir)
		c.Assert(err, tc.ErrorIsNil)
		c.Check(entries, tc.HasLen, 0)
	}
}

func (s *validateSuite) TestReadArchiveRejectsOversizedModelDump(c *tc.C) {
	// Write only the header: rejection must precede reading the contents,
	// so this test needs neither a large allocation nor a large file.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	c.Assert(tw.WriteHeader(&tar.Header{
		Name:     "juju-backup/dump/models/" + testModelAUUID + ".yaml",
		Mode:     0600,
		Size:     recovery.MaxDumpSize + 1,
		Typeflag: tar.TypeReg,
	}), tc.ErrorIsNil)
	c.Assert(gz.Close(), tc.ErrorIsNil)
	archivePath := filepath.Join(c.MkDir(), "oversized.tar.gz")
	c.Assert(os.WriteFile(archivePath, buf.Bytes(), 0600), tc.ErrorIsNil)
	stagingDir := c.MkDir()
	c.Setenv("TMPDIR", stagingDir)

	_, _, err := recovery.ReadArchive(c.Context(), archivePath, "")
	c.Check(err, tc.ErrorMatches, fmt.Sprintf("reading .*: file exceeds %d bytes", recovery.MaxDumpSize))
	entries, err := os.ReadDir(stagingDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(entries, tc.HasLen, 0)
}

func (s *validateSuite) TestUnpackObjectsBundle(c *tc.C) {
	files := validFiles()
	files["juju-backup/root.tar"] = rootTar(c)
	archivePath, _ := writeArchive(c, files)

	dest := c.MkDir()
	err := recovery.UnpackObjectsBundle(c.Context(), archivePath, dest)
	c.Assert(err, tc.ErrorIsNil)

	content, err := os.ReadFile(filepath.Join(dest, "var", "lib", "juju", "objectstore", "some-ns", "somehash"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(content), tc.Equals, "blob content")
}

func (s *validateSuite) TestUnpackObjectsBundleRejectsTraversal(c *tc.C) {
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

	err = recovery.UnpackObjectsBundle(c.Context(), archivePath, c.MkDir())
	c.Assert(err, tc.ErrorMatches, "object bundle contains unsafe path .*")
}

func (s *validateSuite) TestUnpackObjectsBundleSkipsSymlinks(c *tc.C) {
	// k8s controllers archive their tools binaries as symlinks; the
	// bundle unpacker skips them (recovery installs only referenced
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
	err = recovery.UnpackObjectsBundle(c.Context(), archivePath, dest)
	c.Assert(err, tc.ErrorIsNil)

	got, err := os.ReadFile(filepath.Join(dest, "var", "lib", "juju", "objectstore", "ns", "somehash"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(got), tc.Equals, "blob content")
	if _, err := os.Lstat(filepath.Join(dest, "var", "lib", "juju", "tools")); err == nil {
		c.Errorf("symlink entry was extracted")
	}
}

func (s *validateSuite) TestUnpackObjectsBundleAcceptsTypeRegA(c *tc.C) {
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
	err = recovery.UnpackObjectsBundle(c.Context(), archivePath, dest)
	c.Assert(err, tc.ErrorIsNil)

	got, err := os.ReadFile(filepath.Join(dest, "var", "lib", "juju", "objectstore", "ns", "oldhash"))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(got), tc.Equals, "old-tar blob")
}
