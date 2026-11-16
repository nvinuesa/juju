// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	stdtesting "testing"

	"github.com/juju/tc"

	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/internal/recovery"
	"github.com/juju/juju/internal/testhelpers"
)

type objectsSuite struct {
	testhelpers.IsolationSuite
}

func TestObjectsSuite(t *stdtesting.T) {
	tc.Run(t, &objectsSuite{})
}

const testSHA384 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func (s *objectsSuite) TestUnpackObjectStoreInstallsBlobs(c *tc.C) {
	inner := objectStoreTar(c, map[string]string{
		"home/ubuntu/data/objectstore/controller/" + testSHA384: "blob contents",
	})
	path := filepath.Join(c.MkDir(), "juju-backup.tar.gz")
	c.Assert(os.WriteFile(path, makeArchive(c, map[string][]byte{
		"juju-backup/root.tar": inner,
	}), 0o600), tc.ErrorIsNil)
	dataDir := c.MkDir()

	installed, err := recovery.UnpackObjectStore(c.Context(), path, dataDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(installed, tc.Equals, 1)

	contents, err := os.ReadFile(filepath.Join(
		dataDir, "objectstore", "controller", testSHA384))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(contents), tc.Equals, "blob contents")
}

func (s *objectsSuite) TestUnpackObjectStoreRejectsUnsafePath(c *tc.C) {
	inner := objectStoreTar(c, map[string]string{
		"objectstore/../escape": "blob contents",
	})
	path := filepath.Join(c.MkDir(), "juju-backup.tar.gz")
	c.Assert(os.WriteFile(path, makeArchive(c, map[string][]byte{
		"juju-backup/root.tar": inner,
	}), 0o600), tc.ErrorIsNil)

	_, err := recovery.UnpackObjectStore(c.Context(), path, c.MkDir())
	c.Check(err, tc.ErrorMatches, `object archive contains unsafe path "objectstore/../escape"`)
}

func (s *objectsSuite) TestUnpackObjectStoreRequiresFilesArchive(c *tc.C) {
	path := filepath.Join(c.MkDir(), "juju-backup.tar.gz")
	c.Assert(os.WriteFile(path, makeArchive(c, map[string][]byte{
		"juju-backup/metadata.json": []byte("{}"),
	}), 0o600), tc.ErrorIsNil)

	_, err := recovery.UnpackObjectStore(c.Context(), path, c.MkDir())
	c.Check(err, tc.ErrorMatches, "archive is missing juju-backup/root.tar")
}

func (s *objectsSuite) TestCheckObjectStoreRequiresEveryBlob(c *tc.C) {
	dataDir := c.MkDir()
	inventories := []domainrecovery.ObjectInventory{{
		Namespace: "controller",
		Paths: []domainrecovery.ObjectMetadataPath{
			{Path: "charm.ash", SHA384: testSHA384},
		},
	}}

	err := recovery.CheckObjectStore(c.Context(), inventories, dataDir)
	c.Check(err, tc.ErrorMatches,
		"recovered object metadata references blobs that are not in the archive: controller/charm.ash")

	blobDir := filepath.Join(dataDir, "objectstore", "controller")
	c.Assert(os.MkdirAll(blobDir, 0o700), tc.ErrorIsNil)
	c.Assert(os.WriteFile(filepath.Join(blobDir, testSHA384), []byte("blob"), 0o600), tc.ErrorIsNil)
	c.Check(recovery.CheckObjectStore(c.Context(), inventories, dataDir), tc.ErrorIsNil)
}

func (s *objectsSuite) TestCheckObjectStoreRejectsInvalidHash(c *tc.C) {
	err := recovery.CheckObjectStore(c.Context(), []domainrecovery.ObjectInventory{{
		Namespace: "controller",
		Paths:     []domainrecovery.ObjectMetadataPath{{SHA384: "not-a-hash"}},
	}}, c.MkDir())
	c.Check(err, tc.ErrorMatches,
		`recovered object metadata references an invalid blob hash "not-a-hash".*`)
}

// objectStoreTar builds a tar stream with the given name/content entries.
func objectStoreTar(c *tc.C, entries map[string]string) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		contents := entries[name]
		c.Assert(tw.WriteHeader(&tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Mode:     0o600,
			Size:     int64(len(contents)),
		}), tc.ErrorIsNil)
		_, err := tw.Write([]byte(contents))
		c.Assert(err, tc.ErrorIsNil)
	}
	c.Assert(tw.Close(), tc.ErrorIsNil)
	return buf.Bytes()
}
