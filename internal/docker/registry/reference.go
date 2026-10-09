// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package registry

import (
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/distribution/reference"

	"github.com/juju/juju/internal/docker"
	"github.com/juju/juju/internal/errors"
)

// ResolveReference verifies a Linux platform and returns its immutable image
// reference. A canonical input is fetched by digest, never by a mutable tag.
func ResolveReference(ctx context.Context, image string, details docker.ImageRepoDetails, architecture string) (string, error) {
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return "", errors.Capture(err)
	}
	var selector string
	if canonical, ok := ref.(reference.Canonical); ok {
		selector = canonical.Digest().String()
	} else if tagged, ok := ref.(reference.Tagged); ok {
		selector = tagged.Tag()
	} else {
		return "", errors.New("image requires an explicit tag or digest")
	}
	if details.IsPrivate() && details.Repository != "" {
		repo, err := reference.ParseNormalizedNamed(details.Repository)
		if err != nil {
			return "", errors.Capture(err)
		}
		if reference.Domain(repo) != reference.Domain(ref) {
			return "", errors.New("image credentials belong to a different registry")
		}
	}
	if details.ServerAddress != "" {
		address := details.ServerAddress
		if strings.HasPrefix(address, "//") {
			address = "https:" + address
		} else if !strings.Contains(address, "://") {
			address = "https://" + address
		}
		endpoint, err := url.Parse(address)
		if err != nil || endpoint.Host != reference.Domain(ref) {
			return "", errors.New("image registry endpoint disagrees with the image reference")
		}
	}
	name := path.Base(ref.Name())
	details.Repository = strings.TrimSuffix(ref.Name(), "/"+name)
	digest, err := ResolveImage(ctx, details, name, selector, architecture)
	if err != nil {
		return "", errors.Capture(err)
	}
	return ref.Name() + "@" + digest, nil
}
