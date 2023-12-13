// Copyright 2020 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package space

import (
	"github.com/juju/errors"

	"github.com/juju/juju/state"
)

type spaceStateShim struct {
	*state.State
}

// NewState creates a space state shim.
func NewState(st *state.State) *spaceStateShim {
	return &spaceStateShim{
		st,
	}
}

func (s *spaceStateShim) ConstraintsBySpaceName(name string) ([]Constraints, error) {
	constraints, err := s.State.ConstraintsBySpaceName(name)
	if err != nil {
		return nil, errors.Trace(err)
	}

	results := make([]Constraints, len(constraints))
	for i, constraint := range constraints {
		results[i] = constraint
	}
	return results, nil
}
