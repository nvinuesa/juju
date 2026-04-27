// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package adapter

import (
	"context"

	"github.com/juju/juju/internal/errors"
)

// Adapter walks a payload through a chain of version-to-version
// transformers to bring it up to a target schema format version.
//
// The chain is a linear sequence of [Registration] values, one per
// adjacent pair in a caller-supplied version list. [New] validates the
// chain at construction so the controller refuses to start when a step
// is missing.
type Adapter struct {
	// versions is the ordered list of schema format versions the adapter
	// knows about. The last entry is the target.
	versions []string
	// chain maps a source version to its transformer entry. The target
	// version has no entry (nothing to run).
	chain  map[string]Registration
	target string
}

// New builds an Adapter from the given transformer registrations and the
// ordered list of schema format versions. Invoked at controller startup;
// returns an error if the chain is not well-formed: missing step,
// duplicate step, or no versions configured.
func New(regs []Registration, versions []string) (*Adapter, error) {
	if len(versions) == 0 {
		return nil, errors.Errorf("no export versions defined")
	}

	chain := make(map[string]Registration, len(regs))
	for _, r := range regs {
		if _, dup := chain[r.from]; dup {
			return nil, errors.Errorf("%w: %s -> %s", ErrDuplicateTransformer, r.from, r.to)
		}
		chain[r.from] = r
	}

	for i := 0; i < len(versions)-1; i++ {
		from, to := versions[i], versions[i+1]
		r, ok := chain[from]
		if !ok || r.to != to {
			return nil, errors.Errorf("%w: %s -> %s", ErrMissingTransformer, from, to)
		}
	}

	return &Adapter{
		versions: append([]string(nil), versions...),
		chain:    chain,
		target:   versions[len(versions)-1],
	}, nil
}

// Adapt walks payload forward from srcVersion to the adapter's target
// version, applying one registered transformer per step. Each step's
// expected Src type is verified against payload's runtime type before
// invocation (see [register]). If any step fails, the returned error is
// wrapped with the failing (from -> to) pair.
//
// If srcVersion equals the target, payload is returned unchanged.
func (a *Adapter) Adapt(ctx context.Context, srcVersion string, payload any, deps Deps) (any, error) {
	if srcVersion == a.target {
		return payload, nil
	}

	if a.indexOf(srcVersion) < 0 {
		return nil, errors.Errorf("%w: %s", ErrUnknownSourceVersion, srcVersion)
	}

	current := srcVersion
	cur := payload
	for current != a.target {
		r, ok := a.chain[current]
		if !ok {
			return nil, errors.Errorf("%w: %s -> ?", ErrMissingTransformer, current)
		}
		next, err := r.transform(ctx, cur, deps)
		if err != nil {
			return nil, errors.Errorf("transforming %s -> %s: %w", r.from, r.to, err)
		}
		cur = next
		current = r.to
	}
	return cur, nil
}

// Target returns the schema format version this adapter walks payloads up to.
func (a *Adapter) Target() string {
	return a.target
}

func (a *Adapter) indexOf(v string) int {
	for i, x := range a.versions {
		if x == v {
			return i
		}
	}
	return -1
}
