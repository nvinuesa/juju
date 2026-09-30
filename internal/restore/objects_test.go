// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore_test

import (
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/juju/tc"

	"github.com/juju/juju/internal/restore"
)

func (s *loadSuite) writeBlob(c *tc.C, root, namespace, content string) string {
	dir := filepath.Join(root, "var", "lib", "juju", "objectstore", namespace)
	c.Assert(os.MkdirAll(dir, 0o700), tc.ErrorIsNil)
	sum := sha512.Sum384([]byte(content))
	hash := hex.EncodeToString(sum[:])
	c.Assert(os.WriteFile(filepath.Join(dir, hash), []byte(content), 0o600), tc.ErrorIsNil)
	return hash
}

func (s *loadSuite) insertObjectMetadata(c *tc.C, uuid, sha384 string, size int64) {
	_, err := s.DB().Exec(
		"INSERT INTO object_store_metadata (uuid, sha_256, sha_384, size) VALUES (?, ?, ?, ?)",
		uuid, "ignored", sha384, size)
	c.Assert(err, tc.ErrorIsNil)
}

func (s *loadSuite) TestLoadObjects(c *tc.C) {
	bundle := c.MkDir()
	dataDir := c.MkDir()

	blobHash := s.writeBlob(c, bundle, "model-ns", "charm blob content")
	s.insertObjectMetadata(c, "metadata-uuid-1", blobHash, int64(len("charm blob content")))

	copied, err := restore.LoadObjects(c.Context(), []restore.ObjectDB{{Namespace: "controller", DB: s.DB()}}, bundle, dataDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(copied, tc.Equals, 1)

	installed, err := os.ReadFile(filepath.Join(dataDir, "objectstore", "model-ns", blobHash))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(installed), tc.Equals, "charm blob content")

	info, err := os.Stat(filepath.Join(dataDir, "objectstore", "model-ns", blobHash))
	c.Assert(err, tc.ErrorIsNil)
	c.Check(info.Mode().Perm(), tc.Equals, os.FileMode(0o600))
}

func (s *loadSuite) TestLoadObjectsSkipsExistingIdentical(c *tc.C) {
	bundle := c.MkDir()
	dataDir := c.MkDir()

	blobHash := s.writeBlob(c, bundle, "model-ns", "same content")
	// The replacement already holds the same blob in its own object
	// store (target layout: dataDir/objectstore).
	existingDir := filepath.Join(dataDir, "objectstore", "model-ns")
	c.Assert(os.MkdirAll(existingDir, 0o700), tc.ErrorIsNil)
	c.Assert(os.WriteFile(filepath.Join(existingDir, blobHash), []byte("same content"), 0o600), tc.ErrorIsNil)
	s.insertObjectMetadata(c, "metadata-uuid-1", blobHash, int64(len("same content")))

	copied, err := restore.LoadObjects(c.Context(), []restore.ObjectDB{{Namespace: "controller", DB: s.DB()}}, bundle, dataDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(copied, tc.Equals, 0)
}

func (s *loadSuite) TestLoadObjectsMissingBlobFails(c *tc.C) {
	bundle := c.MkDir()
	dataDir := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(bundle, "var", "lib", "juju", "objectstore"), 0o700), tc.ErrorIsNil)
	s.insertObjectMetadata(c, "metadata-uuid-1", "abcdef", 10)

	_, err := restore.LoadObjects(c.Context(), []restore.ObjectDB{{Namespace: "controller", DB: s.DB()}}, bundle, dataDir)
	c.Assert(err, tc.ErrorMatches,
		`object blob "abcdef" referenced by metadata is not in the archive`)
}

func (s *loadSuite) TestLoadObjectsCorruptBlobFails(c *tc.C) {
	bundle := c.MkDir()
	dataDir := c.MkDir()

	blobHash := s.writeBlob(c, bundle, "model-ns", "good content")
	// Corrupt the bundle blob without changing its name.
	blobPath := filepath.Join(bundle, "var", "lib", "juju", "objectstore", "model-ns", blobHash)
	c.Assert(os.WriteFile(blobPath, []byte("corrupted!"), 0o600), tc.ErrorIsNil)
	s.insertObjectMetadata(c, "metadata-uuid-1", blobHash, int64(len("good content")))

	_, err := restore.LoadObjects(c.Context(), []restore.ObjectDB{{Namespace: "controller", DB: s.DB()}}, bundle, dataDir)
	c.Assert(err, tc.ErrorMatches, ".*fails size or hash verification")
}

func (s *loadSuite) TestLoadObjectsMultiNamespace(c *tc.C) {
	bundle := c.MkDir()
	dataDir := c.MkDir()

	// The same blob placed in two namespaces (equal hashes remain
	// distinct placements) is installed into both.
	blobHash := s.writeBlob(c, bundle, "ns-a", "shared content")
	s.writeBlob(c, bundle, "ns-b", "shared content")
	s.insertObjectMetadata(c, "metadata-uuid-1", blobHash, int64(len("shared content")))

	copied, err := restore.LoadObjects(c.Context(), []restore.ObjectDB{{Namespace: "controller", DB: s.DB()}}, bundle, dataDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(copied, tc.Equals, 2)

	for _, ns := range []string{"ns-a", "ns-b"} {
		_, err := os.Stat(filepath.Join(dataDir, "objectstore", ns, blobHash))
		c.Check(err, tc.ErrorIsNil)
	}
}

func (s *loadSuite) insertObjectMetadataPath(c *tc.C, path, metadataUUID string) {
	_, err := s.DB().Exec(
		"INSERT INTO object_store_metadata_path (path, metadata_uuid) VALUES (?, ?)",
		path, metadataUUID)
	c.Assert(err, tc.ErrorIsNil)
}

func (s *loadSuite) TestLoadObjectsMissingPathBlobFails(c *tc.C) {
	bundle := c.MkDir()
	dataDir := c.MkDir()

	// The blob exists in the bundle under a foreign namespace: the
	// controller database's own namespace directory gets nothing, so
	// the merged metadata_path row would only fail on first Get.
	blobHash := s.writeBlob(c, bundle, "ns-foreign", "path blob content")
	s.insertObjectMetadata(c, "metadata-uuid-1", blobHash, int64(len("path blob content")))
	s.insertObjectMetadataPath(c, "charms/foo", "metadata-uuid-1")

	_, err := restore.LoadObjects(c.Context(),
		[]restore.ObjectDB{{Namespace: "controller", DB: s.DB()}}, bundle, dataDir)
	c.Assert(err, tc.ErrorMatches,
		`restored object metadata references blobs that are not in the archive: controller/charms/foo`)
}

func (s *loadSuite) TestLoadObjectsPathBlobVerified(c *tc.C) {
	bundle := c.MkDir()
	dataDir := c.MkDir()

	blobHash := s.writeBlob(c, bundle, "controller", "path blob content")
	s.insertObjectMetadata(c, "metadata-uuid-1", blobHash, int64(len("path blob content")))
	s.insertObjectMetadataPath(c, "charms/foo", "metadata-uuid-1")

	copied, err := restore.LoadObjects(c.Context(),
		[]restore.ObjectDB{{Namespace: "controller", DB: s.DB()}}, bundle, dataDir)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(copied, tc.Equals, 1)
}

func (s *loadSuite) TestLoadObjectsSizeConflictFails(c *tc.C) {
	bundle := c.MkDir()
	dataDir := c.MkDir()

	// Two databases record the same sha384 with different sizes: the
	// archive is inconsistent and the load must fail instead of
	// silently keeping one of the rows.
	blobHash := s.writeBlob(c, bundle, "ns-a", "shared content")
	s.insertObjectMetadata(c, "metadata-uuid-1", blobHash, int64(len("shared content")))
	second := s.openModelDB(c, sourceModelUUID)
	defer second.Close()
	_, err := second.Exec(
		"INSERT INTO object_store_metadata (uuid, sha_256, sha_384, size) VALUES (?, ?, ?, ?)",
		"metadata-uuid-2", "ignored", blobHash, int64(len("shared content")+1))
	c.Assert(err, tc.ErrorIsNil)

	_, err = restore.LoadObjects(c.Context(), []restore.ObjectDB{
		{Namespace: "controller", DB: s.DB()},
		{Namespace: sourceModelUUID, DB: second},
	}, bundle, dataDir)
	c.Assert(err, tc.ErrorMatches, `object metadata conflict: sha384 ".*" is recorded with size .* and .*`)
}
