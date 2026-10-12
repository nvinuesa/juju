// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"
	"testing"

	"github.com/juju/tc"

	coremodel "github.com/juju/juju/core/model"
	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/internal/errors"
)

type serviceSuite struct{}

func TestServiceSuite(t *testing.T) { tc.Run(t, &serviceSuite{}) }

// Embedding the contract makes unexpected persistence calls fail the test.
type recordingState struct {
	State
	controller *domainrecovery.ControllerDump
	model      *domainrecovery.ModelDump
	loadErr    error
}

func (st *recordingState) LoadControllerDump(_ context.Context, dump *domainrecovery.ControllerDump) error {
	st.controller = dump
	return st.loadErr
}

func (st *recordingState) LoadModelDump(_ context.Context, dump *domainrecovery.ModelDump) error {
	st.model = dump
	return st.loadErr
}

const controllerYAML = `version: 4.1.0
payload:
  controller:
  - uuid: source
    model_uuid: model
    target_version: 4.1.0
`

func (s *serviceSuite) TestRequiredDependencies(c *tc.C) {
	_, err := NewService(nil, func(context.Context, string) (State, error) { return &recordingState{}, nil })
	c.Assert(err, tc.ErrorMatches, "recovery requires controller state and a model state factory")
	_, err = NewService(&recordingState{}, nil)
	c.Assert(err, tc.ErrorMatches, "recovery requires controller state and a model state factory")
}

func (s *serviceSuite) TestControllerPoliciesAndBackupLeases(c *tc.C) {
	st := &recordingState{}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil })
	c.Assert(err, tc.ErrorIsNil)
	data := []byte(controllerYAML + `  controller_node: []
  object_store_placement: []
  lease_type:
  - {id: 1, type: application-leadership}
  - {id: 2, type: backup-creation}
  lease:
  - {uuid: leader, lease_type_id: 1}
  - {uuid: backup, lease_type_id: 2}
  lease_pin:
  - {uuid: leader-pin, lease_uuid: leader}
  - {uuid: backup-pin, lease_uuid: backup}
`)
	for range 2 {
		err := svc.LoadControllerDump(c.Context(), data)
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(st.controller, tc.NotNil)
		c.Check(st.controller.Present["controller_node"], tc.IsFalse)
		c.Check(st.controller.Present["object_store_placement"], tc.IsFalse)
		c.Assert(st.controller.ControllerLease, tc.HasLen, 1)
		c.Check(*st.controller.ControllerLease[0].UUID, tc.Equals, "leader")
		c.Assert(st.controller.ControllerLeasePin, tc.HasLen, 1)
		c.Check(*st.controller.ControllerLeasePin[0].UUID, tc.Equals, "leader-pin")
		c.Check(st.controller.ControllerLeaseType, tc.HasLen, 2)
	}
}

func (s *serviceSuite) TestUnknownTableAndColumnRejectedBeforeWrite(c *tc.C) {
	for _, extra := range []string{
		"  unknown_table: []\n",
		"  cloud:\n  - {uuid: cloud, unknown_column: value}\n",
		"  controller_node:\n  - {unknown_column: value}\n",
		"  lease_type:\n  - {id: 2, type: backup-creation}\n  lease:\n  - {uuid: backup, lease_type_id: 2, unknown_column: value}\n",
	} {
		st := &recordingState{}
		svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil })
		c.Assert(err, tc.ErrorIsNil)
		err = svc.LoadControllerDump(c.Context(), []byte(controllerYAML+extra))
		c.Assert(err, tc.NotNil)
		c.Check(st.controller, tc.IsNil)
	}
}

func (s *serviceSuite) TestModelUUIDValidatedBeforeOpening(c *tc.C) {
	opened := false
	svc, err := NewService(&recordingState{}, func(context.Context, string) (State, error) {
		opened = true
		return &recordingState{}, nil
	})
	c.Assert(err, tc.ErrorIsNil)
	err = svc.LoadModelDump(c.Context(), coremodel.UUID("invalid"), []byte("version: 4.1.0\npayload: {net_node: []}"))
	c.Assert(err, tc.NotNil)
	c.Check(opened, tc.IsFalse)
}

func (s *serviceSuite) TestModelStateReusedAndEmptyTablePreserved(c *tc.C) {
	st := &recordingState{}
	opened := 0
	svc, err := NewService(st, func(context.Context, string) (State, error) { opened++; return st, nil })
	c.Assert(err, tc.ErrorIsNil)
	model := coremodel.UUID("33333333-0000-0000-0000-000000000000")
	for range 2 {
		err := svc.LoadModelDump(c.Context(), model, []byte("version: 4.1.0\npayload: {net_node: [], k8s_pod: []}"))
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(st.model, tc.NotNil)
		c.Check(st.model.Present["net_node"], tc.IsTrue)
		c.Check(st.model.Present["machine"], tc.IsFalse)
		c.Check(st.model.Present["k8s_pod"], tc.IsFalse)
	}
	c.Check(opened, tc.Equals, 1)
}

func (s *serviceSuite) TestFactoryFailureAndCancellation(c *tc.C) {
	failure := errors.New("open failed")
	svc, err := NewService(&recordingState{}, func(context.Context, string) (State, error) { return nil, failure })
	c.Assert(err, tc.ErrorIsNil)
	model := coremodel.UUID("33333333-0000-0000-0000-000000000000")
	data := []byte("version: 4.1.0\npayload: {net_node: []}")
	err = svc.LoadModelDump(c.Context(), model, data)
	c.Assert(err, tc.ErrorIs, failure)
	ctx, cancel := context.WithCancel(c.Context())
	cancel()
	err = svc.LoadModelDump(ctx, model, data)
	c.Assert(err, tc.ErrorIs, context.Canceled)
	err = svc.LoadControllerDump(ctx, []byte(controllerYAML))
	c.Assert(err, tc.ErrorIs, context.Canceled)
}
