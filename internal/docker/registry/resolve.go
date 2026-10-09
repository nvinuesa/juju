// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package registry

import (
	"context"

	"github.com/juju/juju/internal/docker"
	"github.com/juju/juju/internal/errors"
)

// ResolveImage resolves an exact tag to an immutable manifest digest after
// checking the requested Linux platform. Authentication and connections have
// the same lifecycle as other registry operations.
func ResolveImage(ctx context.Context, details docker.ImageRepoDetails, image, tag, architecture string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", errors.Capture(err)
	}
	client, err := New(details)
	if err != nil {
		return "", errors.Capture(err)
	}
	defer client.Close()
	if err := client.RefreshAuth(); err != nil {
		return "", errors.Capture(err)
	}
	resolver, ok := client.(interface {
		ResolveDigest(context.Context, string, string, string) (string, error)
	})
	if !ok {
		return "", errors.New("registry cannot resolve immutable image manifests")
	}
	return resolver.ResolveDigest(ctx, image, tag, architecture)
}
