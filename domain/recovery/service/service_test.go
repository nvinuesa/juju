// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"
	"testing"

	"github.com/juju/tc"

	coredatabase "github.com/juju/juju/core/database"
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
	machinePatch   *domainrecovery.MachinePatch
	unitMachine    string
	unitPatch      *domainrecovery.ControllerUnitAddressPatch
	credential     map[string]string
	machineName    string
	objectPaths    []domainrecovery.ObjectMetadataPath
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

func (st *recordingState) GetControllerMachineName(context.Context) (string, error) {
	return st.machineName, st.loadErr
}

func (st *recordingState) PatchControllerMachine(_ context.Context, patch domainrecovery.MachinePatch) error {
	st.machinePatch = &patch
	return st.loadErr
}

func (st *recordingState) PatchControllerUnitAddresses(_ context.Context, machineName string, patch domainrecovery.ControllerUnitAddressPatch) error {
	st.unitMachine = machineName
	st.unitPatch = &patch
	return st.loadErr
}

func (st *recordingState) PatchControllerCredential(_ context.Context, _, authType string, attributes map[string]string) error {
	st.credential = attributes
	return st.loadErr
}

func (st *recordingState) GetObjectMetadataPaths(context.Context) ([]domainrecovery.ObjectMetadataPath, error) {
	return st.objectPaths, st.loadErr
}

const (
	controllerModelUUID = "c0ffee00-aaaa-2222-3333-444455556666"

	controllerYAML = `version: 4.1.0
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
)

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

func (s *serviceSuite) TestPatchControllerMachineResolvesArchivedName(c *tc.C) {
	st := &recordingState{machineName: "1"}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)
	model := coremodel.UUID(controllerModelUUID)

	name, err := svc.PatchControllerMachine(c.Context(), model, domainrecovery.MachinePatch{
		InstanceID: "replacement-instance",
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(name, tc.Equals, "1")
	c.Assert(st.machinePatch, tc.NotNil)
	c.Check(st.machinePatch.MachineName, tc.Equals, "1")
	c.Check(st.machinePatch.InstanceID, tc.Equals, "replacement-instance")
}

func (s *serviceSuite) TestPatchControllerMachineHonoursPresetName(c *tc.C) {
	st := &recordingState{machineName: "1"}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)

	name, err := svc.PatchControllerMachine(c.Context(), coremodel.UUID(controllerModelUUID), domainrecovery.MachinePatch{
		MachineName: "7",
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(name, tc.Equals, "7")
	c.Check(st.machinePatch.MachineName, tc.Equals, "7")
}

func (s *serviceSuite) TestPatchControllerUnitAddressesBuildsPatch(c *tc.C) {
	st := &recordingState{}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)

	err = svc.PatchControllerUnitAddresses(c.Context(), coremodel.UUID(controllerModelUUID), "0", []string{
		"10.0.0.5", "controller-0.example",
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(st.unitMachine, tc.Equals, "0")
	c.Assert(st.unitPatch, tc.NotNil)
	c.Assert(st.unitPatch.Addresses, tc.HasLen, 2)
	c.Check(st.unitPatch.Addresses[0].Value, tc.Equals, "10.0.0.5")
	c.Check(st.unitPatch.Addresses[1].Value, tc.Equals, "controller-0.example")
	c.Check(st.unitPatch.DeviceUUID, tc.Not(tc.Equals), "")
}

func (s *serviceSuite) TestPatchControllerCredential(c *tc.C) {
	st := &recordingState{}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)

	err = svc.PatchControllerCredential(c.Context(), controllerModelUUID, "access-key",
		map[string]string{"key": "value"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(st.credential, tc.DeepEquals, map[string]string{"key": "value"})
}

func (s *serviceSuite) TestObjectInventoryCoversControllerAndModels(c *tc.C) {
	paths := []domainrecovery.ObjectMetadataPath{{Path: "charm.ash", SHA384: "hash"}}
	st := &recordingState{objectPaths: paths}
	svc, err := NewService(st, func(context.Context, string) (State, error) { return st, nil }, "bootstrap-node")
	c.Assert(err, tc.ErrorIsNil)

	inventories, err := svc.GetObjectInventory(c.Context(), []coremodel.UUID{controllerModelUUID})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(inventories, tc.HasLen, 2)
	c.Check(inventories[0].Namespace, tc.Equals, coredatabase.ControllerNS)
	c.Check(inventories[1].Namespace, tc.Equals, controllerModelUUID)
	c.Check(inventories[1].Paths, tc.DeepEquals, paths)
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
