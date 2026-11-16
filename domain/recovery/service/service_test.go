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
	controller     *domainrecovery.ControllerDump
	controllerNode string
	model          *domainrecovery.ModelDump
	modelNode      string
	loadErr        error
}

func (st *recordingState) LoadControllerDump(_ context.Context, dump *domainrecovery.ControllerDump, nodeID string) error {
	st.controller = dump
	st.controllerNode = nodeID
	return st.loadErr
}

func (st *recordingState) LoadModelDump(_ context.Context, dump *domainrecovery.ModelDump, nodeID string) error {
	st.model = dump
	st.modelNode = nodeID
	return st.loadErr
}

const controllerYAML = `version: 4.1.0
payload:
  controller:
  - uuid: source
    model_uuid: model
    target_version: 4.1.0
    api_port: null
    cert: null
    ca_cert: null
    private_key: null
    ca_private_key: null
    system_identity: null
`

func (s *serviceSuite) TestRequiredDependencies(c *tc.C) {
	_, err := NewService(nil, func(context.Context, string) (State, error) { return &recordingState{}, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorMatches, "recovery requires controller state and a model state factory")
	_, err = NewService(&recordingState{}, nil, "bootstrap-node")
	c.Assert(err, tc.ErrorMatches, "recovery requires controller state and a model state factory")
}

func (s *serviceSuite) TestControllerPolicies(c *tc.C) {
	st := &recordingState{}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)
	data := []byte(controllerYAML + `  controller_node: []
  controller_api_address: []
  object_store_placement: []
  flag:
  - {name: bootstrap, value: true, description: ""}
`)
	for range 2 {
		err := svc.LoadControllerDump(c.Context(), data)
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(st.controller, tc.NotNil)
		c.Check(st.controller.Present["controller_node"], tc.IsFalse)
		c.Check(st.controller.Present["controller_api_address"], tc.IsFalse)
		c.Check(st.controller.Present["object_store_placement"], tc.IsFalse)
		c.Check(st.controller.Present["flag"], tc.IsFalse)
		c.Check(st.controller.Present["controller"], tc.IsTrue)
		c.Check(st.controllerNode, tc.Equals, "bootstrap-node")
	}
}

func (s *serviceSuite) TestControllerLoadRequiresControllerRow(c *tc.C) {
	st := &recordingState{}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)
	err = svc.LoadControllerDump(c.Context(), []byte(`version: 4.1.0
payload:
  flag:
  - {name: bootstrap, value: true}
`))
	c.Check(err, tc.ErrorMatches, "controller dump has no controller row")
	c.Check(st.controller, tc.IsNil)
}

func (s *serviceSuite) TestControllerLoadRejectsUnsupportedVersion(c *tc.C) {
	st := &recordingState{}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)
	err = svc.LoadControllerDump(c.Context(), []byte(`version: 3.6.0
payload:
  controller: []
`))
	c.Check(err, tc.ErrorMatches, `controller dump: unsupported dump format version "3.6.0".*`)
	c.Check(st.controller, tc.IsNil)
}

func (s *serviceSuite) TestControllerLoadPropagatesFailure(c *tc.C) {
	loadErr := errors.New("load failed")
	st := &recordingState{loadErr: loadErr}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)
	err = svc.LoadControllerDump(c.Context(), []byte(controllerYAML))
	c.Check(err, tc.ErrorIs, loadErr)
}

func (s *serviceSuite) TestModelLoadRejectsUnsupportedVersion(c *tc.C) {
	st := &recordingState{}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)
	err = svc.LoadModelDump(c.Context(), coremodel.UUID("c0ffee00-aaaa-2222-3333-444455556666"),
		[]byte(`version: 3.6.0
payload:
  machine: []
`))
	c.Check(err, tc.ErrorMatches, `model dump: unsupported dump format version "3.6.0".*`)
	c.Check(st.model, tc.IsNil)
}

func (s *serviceSuite) TestModelLoadValidatesUUID(c *tc.C) {
	st := &recordingState{}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)
	err = svc.LoadModelDump(c.Context(), "", []byte("version: 4.1.0\npayload:\n  machine: []\n"))
	c.Check(err, tc.ErrorMatches, ".*")
	c.Check(st.model, tc.IsNil)
}
