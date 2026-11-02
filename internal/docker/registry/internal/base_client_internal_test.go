// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package internal

import (
	"net/url"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/internal/docker"
)

func TestBaseSuite(t *testing.T) {
	tc.Run(t, &commonURLGetterSuite{})
}

type commonURLGetterSuite struct{}

func (s *commonURLGetterSuite) TestRegistryEndpoints(c *tc.C) {
	for _, test := range []struct{ address, url, persisted string }{
		{"localhost:5000", "https://localhost:5000/v2", "localhost:5000"},
		{"example.com", "https://example.com/v2", "example.com"},
		{"https://example.com:5000", "https://example.com:5000/v2", "//example.com:5000"},
		{"http://localhost:5000", "http://localhost:5000/v2", "http://localhost:5000"},
	} {
		details := docker.ImageRepoDetails{ServerAddress: test.address}
		var baseURL url.URL
		err := decideBaseURLCommon(APIVersionV2, &details, &baseURL)
		c.Assert(err, tc.ErrorIsNil)
		c.Check(baseURL.String(), tc.Equals, test.url)
		c.Check(details.ServerAddress, tc.Equals, test.persisted)
	}
}

func (s *commonURLGetterSuite) TestRootPathKeepsTrailingSlash(c *tc.C) {
	baseURL := url.URL{Scheme: "https", Host: "ghcr.io"}
	got := commonURLGetter(APIVersionV2, baseURL, "/")
	c.Assert(got, tc.Equals, "https://ghcr.io/v2/")
}

func (s *commonURLGetterSuite) TestPathWithLeadingVersionPrefix(c *tc.C) {
	baseURL := url.URL{Scheme: "https", Host: "ghcr.io"}

	got := commonURLGetter(
		APIVersionV2,
		baseURL,
		"/v2/jujuqa/jujud-operator/tags/list",
	)
	c.Assert(got, tc.Equals, "https://ghcr.io/v2/jujuqa/jujud-operator/tags/list")
}

func (s *commonURLGetterSuite) TestPathWithVersionPrefixNoLeadingSlash(c *tc.C) {
	baseURL := url.URL{Scheme: "https", Host: "ghcr.io"}

	got := commonURLGetter(
		APIVersionV2,
		baseURL,
		"v2/jujuqa/jujud-operator/tags/list",
	)
	c.Assert(got, tc.Equals, "https://ghcr.io/v2/jujuqa/jujud-operator/tags/list")
}
