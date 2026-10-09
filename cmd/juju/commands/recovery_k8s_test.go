// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/core/version"
	"github.com/juju/juju/internal/docker/registry"
	coretesting "github.com/juju/juju/internal/testing"
)

type k8sRecoverySuite struct{ coretesting.BaseSuite }

func TestK8sRecoverySuite(t *testing.T) { tc.Run(t, &k8sRecoverySuite{}) }

type recoveryImageTransport func(*http.Request) (*http.Response, error)

func (f recoveryImageTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (s *k8sRecoverySuite) TestImageUsesArchiveVersionAndConfiguredRegistry(c *tc.C) {
	archivedVersion := semversion.MustParse("4.1.0.7")
	config := `{"os":"linux","architecture":"arm64"}`
	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(config)))
	manifest := `{"schemaVersion":2,"config":{"digest":"` + configDigest + `"}}`
	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(manifest)))
	cfg := coretesting.FakeControllerConfig()
	cfg[controller.CAASImageRepo] = "example.com/archived"
	for _, clientVersion := range []string{"4.0.0", "4.2.0.9"} {
		s.PatchValue(&version.Current, semversion.MustParse(clientVersion))
		paths := []string{}
		s.PatchValue(&registry.DefaultTransport, recoveryImageTransport(func(req *http.Request) (*http.Response, error) {
			c.Check(req.URL.Host, tc.Equals, "example.com")
			paths = append(paths, req.URL.Path)
			data, mediaType := manifest, "application/vnd.oci.image.manifest.v1+json"
			if strings.Contains(req.URL.Path, "/blobs/") {
				data, mediaType = config, "application/octet-stream"
			}
			return &http.Response{Request: req, StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{mediaType}}, Body: io.NopCloser(strings.NewReader(data))}, nil
		}))
		image, err := recoveryControllerImage(c.Context(), cfg, archivedVersion, "arm64")
		c.Assert(err, tc.ErrorIsNil)
		c.Check(image, tc.Equals, "example.com/archived/jujud-operator@"+manifestDigest)
		c.Check(paths[0], tc.Equals, "/v2/archived/jujud-operator/manifests/4.1.0.7")
	}
}

func (s *k8sRecoverySuite) TestLegacyImageCannotResolveAgainstAnotherRegistry(c *tc.C) {
	cfg := coretesting.FakeControllerConfig()
	cfg[controller.CAASOperatorImagePath] = "actual.example.com/juju/jujud-operator:4.0.0"
	cfg[controller.CAASImageRepo] = `{"repository":"other.example.com/juju","serveraddress":"https://other.example.com"}`
	_, err := recoveryControllerImage(c.Context(), cfg, semversion.MustParse("4.1.0.7"), "amd64")
	c.Check(err, tc.ErrorMatches, "archived image registry endpoint disagrees with the controller image")
	cfg[controller.CAASImageRepo] = `{"repository":"other.example.com/juju","username":"admin","password":"secret"}`
	_, err = recoveryControllerImage(c.Context(), cfg, semversion.MustParse("4.1.0.7"), "amd64")
	c.Check(err, tc.ErrorMatches, "archived image credentials belong to a different registry than the controller image")
}
