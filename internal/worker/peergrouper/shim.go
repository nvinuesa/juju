// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package peergrouper

import (
	"github.com/juju/errors"
	"github.com/juju/mgo/v3"
	"github.com/juju/replicaset/v3"

	"github.com/juju/juju/state"
)

// This file holds code that translates from State
// to the interface expected internally by the worker.

type StateShim struct {
	*state.State
}

func (s StateShim) ModelType() (state.ModelType, error) {
	model, err := s.Model()
	if err != nil {
		return state.ModelTypeIAAS, errors.Trace(err)
	}
	return model.Type(), nil
}

func (s StateShim) ControllerNode(id string) (ControllerNode, error) {
	return s.State.ControllerNode(id)
}

func (s StateShim) ControllerHost(id string) (ControllerHost, error) {
	return s.State.Machine(id)
}

func (s StateShim) RemoveControllerReference(c ControllerNode) error {
	return s.State.RemoveControllerReference(c)
}

// MongoSessionShim wraps a *mgo.Session to conform to the
// MongoSession interface.
type MongoSessionShim struct {
	*mgo.Session
}

func (s MongoSessionShim) CurrentStatus() (*replicaset.Status, error) {
	return replicaset.CurrentStatus(s.Session)
}

func (s MongoSessionShim) CurrentMembers() ([]replicaset.Member, error) {
	return replicaset.CurrentMembers(s.Session)
}

func (s MongoSessionShim) Set(members []replicaset.Member) error {
	return replicaset.Set(s.Session, members)
}

func (s MongoSessionShim) StepDownPrimary() error {
	return replicaset.StepDownPrimary(s.Session)
}

func (s MongoSessionShim) Refresh() {
	s.Session.Refresh()
}
