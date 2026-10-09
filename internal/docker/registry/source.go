// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package registry

import (
	"context"
	"path"
	"strings"

	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"

	"github.com/juju/juju/internal/docker"
	"github.com/juju/juju/internal/errors"
)

// VerifySourceReference accepts runtime manifest and config IDs, and verifies
// their relationship to a source manifest. All lookups use immutable digests.
func VerifySourceReference(ctx context.Context, image, runtime string, details docker.ImageRepoDetails, architecture string) (string, error) {
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return "", err
	}
	canonical, ok := ref.(reference.Canonical)
	if !ok {
		return "", errors.New("source image requires an immutable manifest")
	}
	resolved, err := ResolveReference(ctx, image, details, architecture)
	if err != nil {
		return "", err
	}
	for _, prefix := range []string{"docker-pullable://", "docker://", "containerd://", "cri-o://"} {
		runtime = strings.TrimPrefix(runtime, prefix)
	}
	if runtimeRef, err := reference.ParseNormalizedNamed(runtime); err == nil {
		if observed, ok := runtimeRef.(reference.Canonical); ok {
			runtime = observed.Digest().String()
		}
	}
	observed := digest.Digest(runtime)
	if err := observed.Validate(); err != nil {
		return "", errors.New("source runtime image ID is not a digest")
	}
	if observed == canonical.Digest() {
		return resolved, nil
	}
	resolvedRef, err := reference.ParseNormalizedNamed(resolved)
	if err != nil {
		return "", err
	}
	manifest := resolvedRef.(reference.Canonical).Digest().String()
	if runtime == manifest {
		return resolved, nil
	}
	name := path.Base(resolvedRef.Name())
	details.Repository = strings.TrimSuffix(resolvedRef.Name(), "/"+name)
	client, err := New(details)
	if err != nil {
		return "", err
	}
	defer client.Close()
	if err := client.RefreshAuth(); err != nil {
		return "", err
	}
	reader, ok := client.(interface {
		ImageConfigDigest(context.Context, string, string) (string, error)
	})
	if !ok {
		return "", errors.New("registry cannot verify source image config identity")
	}
	config, err := reader.ImageConfigDigest(ctx, name, manifest)
	if err != nil {
		return "", err
	}
	if runtime != config {
		return "", errors.New("running source image disagrees with its pinned manifest")
	}
	return resolved, nil
}
