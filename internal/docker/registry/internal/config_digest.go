// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package internal

import (
	"context"
	"encoding/json"

	"github.com/juju/juju/internal/errors"
)

// ImageConfigDigest verifies a manifest before exposing its config identity.
func (c *baseClient) ImageConfigDigest(ctx context.Context, image, digest string) (string, error) {
	repo := getRepositoryOnly(c.ImageRepoDetails().Repository)
	return c.imageConfigDigest(ctx, repo+"/"+image, digest)
}

func (c azureContainerRegistry) ImageConfigDigest(ctx context.Context, image, digest string) (string, error) {
	return c.imageConfigDigest(ctx, image, digest)
}

func (c *elasticContainerRegistry) ImageConfigDigest(ctx context.Context, image, digest string) (string, error) {
	return c.imageConfigDigest(ctx, image, digest)
}

func (c *baseClient) imageConfigDigest(ctx context.Context, image, digest string) (string, error) {
	data, _, observed, err := c.readImageObject(ctx, c.url("/%s/manifests/%s", image, digest))
	if err != nil {
		return "", err
	}
	if observed != digest {
		return "", errors.New("source manifest checksum mismatch")
	}
	var manifest struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", errors.Capture(err)
	}
	if !validImageDigest(manifest.Config.Digest) {
		return "", errors.New("invalid source image config digest")
	}
	return manifest.Config.Digest, nil
}
