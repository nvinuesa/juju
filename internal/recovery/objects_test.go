// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/juju/tc"

	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/internal/recovery"
)

// testHash returns a distinct hex SHA-384-shaped value per fill pair.
func testHash(fill string) string {
	return strings.Repeat(fill, 48)
}

// writeStoreBlob places one blob below dataDir's object store root and
// returns its path.
func writeStoreBlob(c *tc.C, dataDir, namespace, hash, content string) string {
	dir := filepath.Join(dataDir, "objectstore", namespace)
	c.Assert(os.MkdirAll(dir, 0o700), tc.ErrorIsNil)
	blob := filepath.Join(dir, hash)
	c.Assert(os.WriteFile(blob, []byte(content), 0o600), tc.ErrorIsNil)
	return blob
}

func (s *loadSuite) TestCheckObjectStoreComplete(c *tc.C) {
	dataDir := c.MkDir()
	writeStoreBlob(c, dataDir, "model-ns", testHash("ab"), "charm blob content")

	err := recovery.CheckObjectStore(c.Context(), []domainrecovery.ObjectInventory{{
		Namespace: "model-ns",
		Paths: []domainrecovery.ObjectMetadataPath{
			{Path: "charms/foo", SHA384: testHash("ab")},
		},
	}}, dataDir)
	c.Assert(err, tc.ErrorIsNil)
}

func (s *loadSuite) TestCheckObjectStoreMissingBlobFails(c *tc.C) {
	dataDir := c.MkDir()

	err := recovery.CheckObjectStore(c.Context(), []domainrecovery.ObjectInventory{{
		Namespace: "controller",
		Paths: []domainrecovery.ObjectMetadataPath{
			{Path: "charms/foo", SHA384: testHash("cd")},
		},
	}}, dataDir)
	c.Assert(err, tc.ErrorMatches,
		`recovered object metadata references blobs that are not in the archive: controller/charms/foo`)
}

func (s *loadSuite) TestCheckObjectStoreDirectoryFails(c *tc.C) {
	dataDir := c.MkDir()
	blob := filepath.Join(dataDir, "objectstore", "controller", testHash("cd"))
	c.Assert(os.MkdirAll(blob, 0o700), tc.ErrorIsNil)

	err := recovery.CheckObjectStore(c.Context(), []domainrecovery.ObjectInventory{{
		Namespace: "controller",
		Paths: []domainrecovery.ObjectMetadataPath{
			{Path: "charms/foo", SHA384: testHash("cd")},
		},
	}}, dataDir)
	c.Assert(err, tc.ErrorMatches,
		`recovered object metadata references blobs that are not in the archive: controller/charms/foo`)
}

func (s *loadSuite) TestCheckObjectStoreAggregatesMissingAcrossNamespaces(c *tc.C) {
	dataDir := c.MkDir()
	writeStoreBlob(c, dataDir, "ns-a", testHash("ab"), "content")

	err := recovery.CheckObjectStore(c.Context(), []domainrecovery.ObjectInventory{
		{
			Namespace: "ns-a",
			Paths: []domainrecovery.ObjectMetadataPath{
				{Path: "charms/ok", SHA384: testHash("ab")},
				{Path: "charms/gone", SHA384: testHash("cd")},
			},
		},
		{
			Namespace: "ns-b",
			Paths: []domainrecovery.ObjectMetadataPath{
				{Path: "agent-binaries/4.0", SHA384: testHash("ef")},
			},
		},
	}, dataDir)
	c.Assert(err, tc.ErrorMatches,
		`recovered object metadata references blobs that are not in the archive: ns-a/charms/gone; ns-b/agent-binaries/4.0`)
}

func (s *loadSuite) TestCheckObjectStorePathlessRowReportsHash(c *tc.C) {
	// A metadata row no path references is still reachable by hash: the
	// completeness check covers it and reports the hash.
	err := recovery.CheckObjectStore(c.Context(), []domainrecovery.ObjectInventory{{
		Namespace: "controller",
		Paths: []domainrecovery.ObjectMetadataPath{
			{Path: "", SHA384: testHash("cd")},
		},
	}}, c.MkDir())
	c.Assert(err, tc.ErrorMatches,
		`recovered object metadata references blobs that are not in the archive: controller/`+testHash("cd"))
}

func (s *loadSuite) TestCheckObjectStoreNoInventory(c *tc.C) {
	// A controller whose metadata records no objects — fresh bootstrap
	// archives — passes with an empty store.
	err := recovery.CheckObjectStore(c.Context(), nil, c.MkDir())
	c.Assert(err, tc.ErrorIsNil)
}

func (s *loadSuite) TestCheckObjectStoreRejectsMalformedHash(c *tc.C) {
	for _, hash := range []string{
		"",
		"../escape",
		"ns-a/blob",
		strings.Repeat("a", 95),
		strings.Repeat("a", 97),
		strings.Repeat("g", 96),
	} {
		err := recovery.CheckObjectStore(c.Context(), []domainrecovery.ObjectInventory{{
			Namespace: "ns-a",
			Paths: []domainrecovery.ObjectMetadataPath{
				{Path: "charms/foo", SHA384: hash},
			},
		}}, c.MkDir())
		c.Check(err, tc.ErrorMatches,
			`recovered object metadata references an invalid blob hash .*`)
	}
}

func (s *loadSuite) TestCheckObjectStoreTraversalCannotMaskMissingBlob(c *tc.C) {
	dataDir := c.MkDir()
	writeStoreBlob(c, dataDir, "ns-a", testHash("ab"), "content")

	err := recovery.CheckObjectStore(c.Context(), []domainrecovery.ObjectInventory{{
		Namespace: "ns-a",
		Paths: []domainrecovery.ObjectMetadataPath{
			{Path: "charms/foo", SHA384: "../ns-a/" + testHash("ab")},
		},
	}}, dataDir)
	c.Assert(err, tc.ErrorMatches,
		`recovered object metadata references an invalid blob hash .*`)
}
