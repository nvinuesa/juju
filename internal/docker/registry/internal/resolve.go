// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package internal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"github.com/juju/juju/core/arch"
	"github.com/juju/juju/internal/errors"
)

// ResolveDigest checks that the exact tag contains the requested Linux
// platform and returns the manifest digest, never the config blob digest.
func (c *baseClient) ResolveDigest(ctx context.Context, image, tag, architecture string) (string, error) {
	repo := getRepositoryOnly(c.ImageRepoDetails().Repository)
	return c.resolveDigest(ctx, repo+"/"+image, tag, architecture)
}

func (c azureContainerRegistry) ResolveDigest(ctx context.Context, image, tag, architecture string) (string, error) {
	return c.resolveDigest(ctx, image, tag, architecture)
}

func (c *elasticContainerRegistry) ResolveDigest(ctx context.Context, image, tag, architecture string) (string, error) {
	return c.resolveDigest(ctx, image, tag, architecture)
}

func (c *baseClient) resolveDigest(ctx context.Context, image, tag, architecture string) (string, error) {
	if image == "" || tag == "" || architecture == "" {
		return "", errors.New("image, exact tag and architecture are required")
	}
	data, contentType, digest, err := c.readImageObject(ctx, c.url("/%s/manifests/%s", image, tag))
	if err != nil {
		return "", err
	}
	if validImageDigest(tag) && tag != digest {
		return "", errors.New("platform manifest checksum mismatch")
	}
	var manifest struct {
		SchemaVersion int `json:"schemaVersion"`
		Config        struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", errors.Capture(err)
	}
	if manifest.SchemaVersion != 2 {
		return "", errors.New("unsupported image manifest schema")
	}
	switch contentType {
	case manifestContentTypeListV2 + "+json", "application/vnd.oci.image.index.v1+json":
		for _, entry := range manifest.Manifests {
			if entry.Platform.OS == "linux" && arch.NormaliseArch(entry.Platform.Architecture) == architecture {
				if !validImageDigest(entry.Digest) {
					return "", errors.New("invalid platform manifest digest")
				}
				resolved, err := c.resolveDigest(ctx, image, entry.Digest, architecture)
				if err != nil {
					return "", err
				}
				return resolved, nil
			}
		}
	case manifestContentTypeV2 + "+json", manifestContentTypeOCIV1 + "+json":
		if !validImageDigest(manifest.Config.Digest) {
			return "", errors.New("invalid image config digest")
		}
		config, _, configDigest, err := c.readImageObject(ctx, c.url("/%s/blobs/%s", image, manifest.Config.Digest))
		if err != nil {
			return "", err
		}
		if configDigest != manifest.Config.Digest {
			return "", errors.New("image config checksum mismatch")
		}
		var platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		}
		if err := json.Unmarshal(config, &platform); err != nil {
			return "", errors.Capture(err)
		}
		if platform.OS == "linux" && arch.NormaliseArch(platform.Architecture) == architecture {
			return digest, nil
		}
	default:
		return "", errors.Errorf("unsupported image manifest media type %q", contentType)
	}
	return "", errors.Errorf("exact image tag %q has no linux/%s platform", tag, architecture)
}

func (c *baseClient) readImageObject(ctx context.Context, url string) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", "", errors.Capture(err)
	}
	for _, contentType := range []string{manifestContentTypeV2 + "+json", manifestContentTypeListV2 + "+json", manifestContentTypeOCIV1 + "+json", "application/vnd.oci.image.index.v1+json"} {
		req.Header.Add("Accept", contentType)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, "", "", errors.Capture(unwrapNetError(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", "", errors.Errorf("image registry returned HTTP %d", response.StatusCode)
	}
	const limit = 4 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", "", errors.Capture(err)
	}
	if len(data) > limit {
		return nil, "", "", errors.New("image registry response too large")
	}
	checksum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(checksum[:])
	if advertised := response.Header.Get("Docker-Content-Digest"); advertised != "" && advertised != digest {
		return nil, "", "", errors.New("image manifest checksum mismatch")
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		return nil, "", "", errors.Capture(err)
	}
	return data, contentType, digest, nil
}

func validImageDigest(digest string) bool {
	if len(digest) != len("sha256:")+64 || digest[:7] != "sha256:" {
		return false
	}
	_, err := hex.DecodeString(digest[7:])
	return err == nil
}
