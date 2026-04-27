// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package adapter

import (
	"context"
	"reflect"

	"github.com/juju/juju/internal/errors"
)

// Registration is the type-erased form of a single version-to-version
// transformer entry. Construct instances with [Register]; pass a slice of
// them to [New] to build an [Adapter]. Exported so top-level wiring
// packages can hold the registered list without an import cycle.
type Registration struct {
	from, to  string
	srcType   reflect.Type // *Src
	dstType   reflect.Type // *Dst
	transform func(ctx context.Context, src any, deps Deps) (any, error)
}

// Register wraps a typed [TransformFunc] into a [Registration] entry.
// Storage erases the generic type parameters; the returned closure checks
// the payload's runtime Go type against Src before invoking fn so the
// erasure boundary stays safe.
func Register[Src, Dst any](from, to string, fn TransformFunc[Src, Dst]) Registration {
	var zeroSrc Src
	var zeroDst Dst
	expected := reflect.TypeOf(&zeroSrc)
	return Registration{
		from:    from,
		to:      to,
		srcType: expected,
		dstType: reflect.TypeOf(&zeroDst),
		transform: func(ctx context.Context, src any, deps Deps) (any, error) {
			typed, ok := src.(*Src)
			if !ok {
				return nil, errors.Errorf("%w: expected %s, got %T",
					ErrPayloadTypeMismatch, expected, src)
			}
			return fn(ctx, typed, deps)
		},
	}
}
