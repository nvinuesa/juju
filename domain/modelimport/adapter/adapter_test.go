// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package adapter

import (
	"context"
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/internal/errors"
)

// Fake payload types. Each version gets its own named struct so the test
// exercises the type-assertion boundary in [Register].
type payloadA struct{ Val int }
type payloadB struct{ Val int }
type payloadC struct{ Val int }

type adapterSuite struct{}

func TestAdapterSuite(t *testing.T) {
	tc.Run(t, &adapterSuite{})
}

func okAtoB(_ context.Context, src *payloadA, _ Deps) (*payloadB, error) {
	return &payloadB{Val: src.Val + 1}, nil
}

func okBtoC(_ context.Context, src *payloadB, _ Deps) (*payloadC, error) {
	return &payloadC{Val: src.Val + 10}, nil
}

func failBtoC(_ context.Context, _ *payloadB, _ Deps) (*payloadC, error) {
	return nil, errors.Errorf("boom")
}

func (s *adapterSuite) TestNewRejectsEmptyVersions(c *tc.C) {
	_, err := New(nil, nil)
	c.Assert(err, tc.ErrorMatches, "no export versions defined")
}

func (s *adapterSuite) TestNewSingleVersionIsValid(c *tc.C) {
	// One version = no transformers needed = a pure pass-through adapter.
	a, err := New(nil, []string{"1.0"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(a.Target(), tc.Equals, "1.0")
}

func (s *adapterSuite) TestNewDetectsMissingTransformer(c *tc.C) {
	_, err := New(nil, []string{"1.0", "1.1"})
	c.Assert(err, tc.ErrorIs, ErrMissingTransformer)
}

func (s *adapterSuite) TestNewDetectsWrongToInChain(c *tc.C) {
	// Registration goes 1.0 -> 1.2 but versions expect 1.0 -> 1.1.
	reg := Register[payloadA, payloadB]("1.0", "1.2", okAtoB)
	_, err := New([]Registration{reg}, []string{"1.0", "1.1"})
	c.Assert(err, tc.ErrorIs, ErrMissingTransformer)
}

func (s *adapterSuite) TestNewDetectsDuplicateTransformer(c *tc.C) {
	reg1 := Register[payloadA, payloadB]("1.0", "1.1", okAtoB)
	reg2 := Register[payloadA, payloadB]("1.0", "1.1", okAtoB)
	_, err := New([]Registration{reg1, reg2}, []string{"1.0", "1.1"})
	c.Assert(err, tc.ErrorIs, ErrDuplicateTransformer)
}

func (s *adapterSuite) TestAdaptPassesThroughWhenSrcIsTarget(c *tc.C) {
	a, err := New(nil, []string{"1.0"})
	c.Assert(err, tc.ErrorIsNil)

	payload := &payloadA{Val: 7}
	got, err := a.Adapt(c.Context(), "1.0", payload, Deps{})
	c.Assert(err, tc.ErrorIsNil)
	// Same pointer — no copy, no transformer ran.
	c.Check(got, tc.Equals, any(payload))
}

func (s *adapterSuite) TestAdaptWalksChain(c *tc.C) {
	regs := []Registration{
		Register[payloadA, payloadB]("1.0", "1.1", okAtoB),
		Register[payloadB, payloadC]("1.1", "1.2", okBtoC),
	}
	a, err := New(regs, []string{"1.0", "1.1", "1.2"})
	c.Assert(err, tc.ErrorIsNil)

	got, err := a.Adapt(c.Context(), "1.0", &payloadA{Val: 1}, Deps{})
	c.Assert(err, tc.ErrorIsNil)
	// 1 + 1 (AtoB) + 10 (BtoC) = 12.
	c.Check(got, tc.DeepEquals, &payloadC{Val: 12})
}

func (s *adapterSuite) TestAdaptRejectsUnknownSource(c *tc.C) {
	regs := []Registration{
		Register[payloadA, payloadB]("1.0", "1.1", okAtoB),
	}
	a, err := New(regs, []string{"1.0", "1.1"})
	c.Assert(err, tc.ErrorIsNil)

	_, err = a.Adapt(c.Context(), "0.9", &payloadA{}, Deps{})
	c.Assert(err, tc.ErrorIs, ErrUnknownSourceVersion)
}


func (s *adapterSuite) TestAdaptRejectsPayloadTypeMismatch(c *tc.C) {
	regs := []Registration{
		Register[payloadA, payloadB]("1.0", "1.1", okAtoB),
	}
	a, err := New(regs, []string{"1.0", "1.1"})
	c.Assert(err, tc.ErrorIsNil)

	// Transformer expects *payloadA, we hand it *payloadB.
	_, err = a.Adapt(c.Context(), "1.0", &payloadB{}, Deps{})
	c.Assert(err, tc.ErrorIs, ErrPayloadTypeMismatch)
}

func (s *adapterSuite) TestAdaptWrapsMidChainErrors(c *tc.C) {
	regs := []Registration{
		Register[payloadA, payloadB]("1.0", "1.1", okAtoB),
		Register[payloadB, payloadC]("1.1", "1.2", failBtoC),
	}
	a, err := New(regs, []string{"1.0", "1.1", "1.2"})
	c.Assert(err, tc.ErrorIsNil)

	_, err = a.Adapt(c.Context(), "1.0", &payloadA{Val: 0}, Deps{})
	c.Assert(err, tc.ErrorMatches, "transforming 1.1 -> 1.2: boom")
}
