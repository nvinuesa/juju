// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package registry_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/internal/docker"
	"github.com/juju/juju/internal/docker/registry"
	coretesting "github.com/juju/juju/internal/testing"
)

type resolveSuite struct{ coretesting.BaseSuite }

func TestResolveSuite(t *testing.T) { tc.Run(t, &resolveSuite{}) }

type resolveTransport func(*http.Request) (*http.Response, error)

func (f resolveTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func objectDigest(data string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(data))) }
func imageResponse(req *http.Request, mediaType, body string) *http.Response {
	return &http.Response{Request: req, StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func (s *resolveSuite) TestSingleManifestPinsManifestRatherThanConfig(c *tc.C) {
	for _, mediaType := range []string{"application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"} {
		config := `{"os":"linux","architecture":"amd64"}`
		manifest := `{"schemaVersion":2,"config":{"digest":"` + objectDigest(config) + `"},"layers":[]}`
		requests := []string{}
		s.PatchValue(&registry.DefaultTransport, resolveTransport(func(req *http.Request) (*http.Response, error) {
			requests = append(requests, req.URL.Path)
			if strings.Contains(req.URL.Path, "/blobs/") {
				return imageResponse(req, "application/octet-stream", config), nil
			}
			return imageResponse(req, mediaType, manifest), nil
		}))
		digest, err := registry.ResolveImage(c.Context(), docker.ImageRepoDetails{Repository: "example.com/juju"}, "jujud-operator", "4.1.0.7", "amd64")
		c.Assert(err, tc.ErrorIsNil)
		c.Check(digest, tc.Equals, objectDigest(manifest))
		c.Check(digest, tc.Not(tc.Equals), objectDigest(config))
		c.Check(requests, tc.DeepEquals, []string{"/v2/juju/jujud-operator/manifests/4.1.0.7", "/v2/juju/jujud-operator/blobs/" + objectDigest(config)})
	}
}

func (s *resolveSuite) TestIndexesCheckLinuxPlatform(c *tc.C) {
	for _, mediaType := range []string{"application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json"} {
		config := `{"os":"linux","architecture":"arm64"}`
		child := `{"schemaVersion":2,"config":{"digest":"` + objectDigest(config) + `"}}`
		manifest := `{"schemaVersion":2,"manifests":[{"digest":"` + objectDigest(child) + `","platform":{"os":"linux","architecture":"arm64"}},{"digest":"` + objectDigest("darwin") + `","platform":{"os":"darwin","architecture":"amd64"}}]}`
		s.PatchValue(&registry.DefaultTransport, resolveTransport(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/blobs/") {
				return imageResponse(req, "application/octet-stream", config), nil
			}
			if strings.HasSuffix(req.URL.Path, objectDigest(child)) {
				return imageResponse(req, "application/vnd.oci.image.manifest.v1+json", child), nil
			}
			c.Check(req.URL.Path, tc.Equals, "/v2/juju/jujud-operator/manifests/4.1.0.7")
			return imageResponse(req, mediaType, manifest), nil
		}))
		for _, architecture := range []string{"arm64", "amd64"} {
			digest, err := registry.ResolveImage(c.Context(), docker.ImageRepoDetails{Repository: "example.com/juju"}, "jujud-operator", "4.1.0.7", architecture)
			if architecture == "amd64" {
				c.Check(err, tc.ErrorMatches, `exact image tag "4.1.0.7" has no linux/amd64 platform`)
				continue
			}
			c.Assert(err, tc.ErrorIsNil)
			c.Check(digest, tc.Equals, objectDigest(child))
		}
	}
}

func (s *resolveSuite) TestMissingTagNeverFallsBack(c *tc.C) {
	calls := 0
	s.PatchValue(&registry.DefaultTransport, resolveTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		c.Check(req.URL.Path, tc.Equals, "/v2/juju/jujud-operator/manifests/4.1.0.7")
		response := imageResponse(req, "application/json", `{"errors":[{"code":"MANIFEST_UNKNOWN","message":"missing"}]}`)
		response.StatusCode = http.StatusNotFound
		return response, nil
	}))
	_, err := registry.ResolveImage(c.Context(), docker.ImageRepoDetails{Repository: "example.com/juju"}, "jujud-operator", "4.1.0.7", "amd64")
	c.Check(err, tc.NotNil)
	c.Check(calls, tc.Equals, 1)
}

func (s *resolveSuite) TestCorruptManifestDigest(c *tc.C) {
	s.PatchValue(&registry.DefaultTransport, resolveTransport(func(req *http.Request) (*http.Response, error) {
		response := imageResponse(req, "application/vnd.oci.image.index.v1+json", `{"schemaVersion":2,"manifests":[]}`)
		response.Header.Set("Docker-Content-Digest", objectDigest("different manifest"))
		return response, nil
	}))
	_, err := registry.ResolveImage(c.Context(), docker.ImageRepoDetails{Repository: "example.com/juju"}, "jujud-operator", "4.1.0", "amd64")
	c.Check(err, tc.ErrorMatches, "image manifest checksum mismatch")
}

func (s *resolveSuite) TestCancelledResolution(c *tc.C) {
	ctx, cancel := context.WithCancel(c.Context())
	cancel()
	s.PatchValue(&registry.DefaultTransport, resolveTransport(func(req *http.Request) (*http.Response, error) {
		c.Fatalf("unexpected registry request")
		return nil, nil
	}))
	_, err := registry.ResolveImage(ctx, docker.ImageRepoDetails{Repository: "example.com/juju"}, "jujud-operator", "4.1.0", "amd64")
	c.Check(err, tc.ErrorIs, context.Canceled)
}
