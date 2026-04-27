// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package adapter

import "context"

// Deps carries target-side services a transformer may need when it cannot
// derive a field from the source payload alone (e.g. looking up a default
// value on the target controller). Fields are added as transformers start
// to require them; keep the surface narrow.
type Deps struct{}

// TransformFunc converts a payload of schema format version Src into a
// payload of schema format version Dst. Implementations are produced per
// version step by a generator-emitted transform.go plus an engineer-written
// deltas.go (see domain/modelimport/adapter/transforms/<pair>).
type TransformFunc[Src, Dst any] func(ctx context.Context, src *Src, deps Deps) (*Dst, error)
