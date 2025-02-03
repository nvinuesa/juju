// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package application

import (
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v6"

	"github.com/juju/juju/apiserver/common/storagecommon"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/core/crossmodel"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/objectstore"
	"github.com/juju/juju/core/status"
	"github.com/juju/juju/internal/charm"
	"github.com/juju/juju/internal/relation"
	"github.com/juju/juju/internal/tools"
	"github.com/juju/juju/state"
)

// Backend defines the state functionality required by the application
// facade. For details on the methods, see the methods on state.State
// with the same names.
type Backend interface {
	Application(string) (Application, error)
	ApplyOperation(state.ModelOperation) error
	AddApplication(state.AddApplicationArgs, objectstore.ObjectStore) (Application, error)
	RemoteApplication(string) (RemoteApplication, error)
	AddRemoteApplication(state.AddRemoteApplicationParams) (RemoteApplication, error)
	AddRelation(...relation.Endpoint) (Relation, error)
	Relation(int) (Relation, error)
	InferEndpoints(...string) ([]relation.Endpoint, error)
	InferActiveRelation(...string) (Relation, error)
	Machine(string) (Machine, error)
	Unit(string) (Unit, error)
	UnitsInError() ([]Unit, error)
	ControllerTag() names.ControllerTag
	OfferConnectionForRelation(string) (OfferConnection, error)
	SaveEgressNetworks(relationKey string, cidrs []string) (state.RelationNetworks, error)

	// ReadSequence is a stop gap to allow the next unit number to be read from mongo
	// so that correctly matching units can be written to dqlite.
	ReadSequence(name string) (int, error)
}

// Application defines a subset of the functionality provided by the
// state.Application type, as required by the application facade. For
// details on the methods, see the methods on state.Application with
// the same names.
type Application interface {
	Name() string
	AddUnit(state.AddUnitParams) (Unit, error)
	AllUnits() ([]Unit, error)
	ApplicationTag() names.ApplicationTag
	CharmURL() (*string, bool)
	CharmOrigin() *state.CharmOrigin
	ClearExposed() error
	CharmConfig() (charm.Settings, error)
	Constraints() (constraints.Value, error)
	DestroyOperation(objectstore.ObjectStore) *state.DestroyApplicationOperation
	EndpointBindings() (Bindings, error)
	ExposedEndpoints() map[string]state.ExposedEndpoint
	Endpoints() ([]relation.Endpoint, error)
	IsExposed() bool
	IsPrincipal() bool
	IsRemote() bool
	SetCharm(state.SetCharmConfig, objectstore.ObjectStore) error
	SetConstraints(constraints.Value) error
	MergeExposeSettings(map[string]state.ExposedEndpoint) error
	UnsetExposeSettings([]string) error
	UpdateCharmConfig(charm.Settings) error
	MergeBindings(*state.Bindings, bool) error
	Relations() ([]Relation, error)
}

// Bindings defines a subset of the functionality provided by the
// state.Bindings type, as required by the application facade. For
// details on the methods, see the methods on state.Bindings with
// the same names.
type Bindings interface {
	Map() map[string]string
	MapWithSpaceNames(network.SpaceInfos) (map[string]string, error)
}

// Charm defines a subset of the functionality provided by the
// state.Charm type, as required by the application facade. For
// details on the methods, see the methods on state.Charm with
// the same names.
type Charm interface {
	CharmMeta
	Config() *charm.Config
	Actions() *charm.Actions
	Revision() int
	URL() string
	Version() string
}

// CharmMeta describes methods that inform charm operation.
type CharmMeta interface {
	Manifest() *charm.Manifest
	Meta() *charm.Meta
}

// Machine defines a subset of the functionality provided by the
// state.Machine type, as required by the application facade. For
// details on the methods, see the methods on state.Machine with
// the same names.
type Machine interface {
	Base() state.Base
	Id() string
	PublicAddress() (network.SpaceAddress, error)
}

// Relation defines a subset of the functionality provided by the
// state.Relation type, as required by the application facade. For
// details on the methods, see the methods on state.Relation with
// the same names.
type Relation interface {
	status.StatusSetter
	Tag() names.Tag
	Destroy(objectstore.ObjectStore) error
	DestroyWithForce(bool, time.Duration) ([]error, error)
	Id() int
	Endpoints() []relation.Endpoint
	RelatedEndpoints(applicationname string) ([]relation.Endpoint, error)
	ApplicationSettings(appName string) (map[string]interface{}, error)
	AllRemoteUnits(appName string) ([]RelationUnit, error)
	Unit(string) (RelationUnit, error)
	Endpoint(string) (relation.Endpoint, error)
	SetSuspended(bool, string) error
	Suspended() bool
	SuspendedReason() string
}

type RelationUnit interface {
	UnitName() string
	InScope() (bool, error)
	Settings() (map[string]interface{}, error)
}

// Unit defines a subset of the functionality provided by the
// state.Unit type, as required by the application facade. For
// details on the methods, see the methods on state.Unit with
// the same names.
type Unit interface {
	Name() string
	Tag() names.Tag
	UnitTag() names.UnitTag
	ApplicationName() string
	Destroy(objectstore.ObjectStore) error
	DestroyOperation(objectstore.ObjectStore) *state.DestroyUnitOperation
	IsPrincipal() bool
	Resolve(retryHooks bool) error
	AgentTools() (*tools.Tools, error)

	AssignedMachineId() (string, error)
	WorkloadVersion() (string, error)
	AssignUnit() error
	AssignWithPlacement(*instance.Placement, network.SpaceInfos) error
	ContainerInfo() (state.CloudContainer, error)
}

// Model defines a subset of the functionality provided by the
// state.Model type, as required by the application facade. For
// details on the methods, see the methods on state.Model with
// the same names.
type Model interface {
	ModelTag() names.ModelTag
	Type() state.ModelType
	// The following methods are required for querying the featureset
	// supported by the model.
	CloudName() string
	CloudCredentialTag() (names.CloudCredentialTag, bool)
	CloudRegion() string
	ControllerUUID() string
}

// Resources defines a subset of the functionality provided by the
// state.Resources type, as required by the application facade. See
// the state.Resources type for details on the methods.
type Resources interface {
	RemovePendingAppResources(string, map[string]string) error
}

type stateShim struct {
	*state.State
}

type modelShim struct {
	*state.Model
}

type StorageInterface interface {
	storagecommon.StorageAccess
	VolumeAccess() storagecommon.VolumeAccess
	FilesystemAccess() storagecommon.FilesystemAccess
}

var getStorageState = func(st *state.State) (StorageInterface, error) {
	m, err := st.Model()
	if err != nil {
		return nil, err
	}
	sb, err := state.NewStorageBackend(st)
	if err != nil {
		return nil, err
	}
	storageAccess := &storageShim{
		StorageAccess: sb,
		va:            sb,
		fa:            sb,
	}
	// CAAS models don't support volume storage yet.
	if m.Type() == state.ModelTypeCAAS {
		storageAccess.va = nil
	}
	return storageAccess, nil
}

type storageShim struct {
	storagecommon.StorageAccess
	fa storagecommon.FilesystemAccess
	va storagecommon.VolumeAccess
}

func (s *storageShim) VolumeAccess() storagecommon.VolumeAccess {
	return s.va
}

func (s *storageShim) FilesystemAccess() storagecommon.FilesystemAccess {
	return s.fa
}

// NewStateApplication converts a state.Application into an Application.
func NewStateApplication(
	st *state.State,
	app *state.Application,
) Application {
	return stateApplicationShim{
		Application: app,
		st:          st,
	}
}

func (s stateShim) Application(name string) (Application, error) {
	a, err := s.State.Application(name)
	if err != nil {
		return nil, err
	}
	return stateApplicationShim{
		Application: a,
		st:          s.State,
	}, nil
}

func (s stateShim) ReadSequence(name string) (int, error) {
	return state.ReadSequence(s.State, name)
}

func (s stateShim) AddApplication(args state.AddApplicationArgs, store objectstore.ObjectStore) (Application, error) {
	a, err := s.State.AddApplication(args, store)
	if err != nil {
		return nil, err
	}
	return stateApplicationShim{
		Application: a,
		st:          s.State,
	}, nil
}

type remoteApplicationShim struct {
	*state.RemoteApplication
}

type RemoteApplication interface {
	Name() string
	SourceModel() names.ModelTag
	Endpoints() ([]relation.Endpoint, error)
	AddEndpoints(eps []charm.Relation) error
	Destroy() error
	DestroyOperation(force bool) *state.DestroyRemoteApplicationOperation
	Status() (status.StatusInfo, error)
	Life() state.Life
}

func (s stateShim) RemoteApplication(name string) (RemoteApplication, error) {
	app, err := s.State.RemoteApplication(name)
	return &remoteApplicationShim{RemoteApplication: app}, err
}

func (s stateShim) AddRemoteApplication(args state.AddRemoteApplicationParams) (RemoteApplication, error) {
	app, err := s.State.AddRemoteApplication(args)
	return &remoteApplicationShim{RemoteApplication: app}, err
}

func (s stateShim) AddRelation(eps ...relation.Endpoint) (Relation, error) {
	r, err := s.State.AddRelation(eps...)
	if err != nil {
		return nil, err
	}
	return stateRelationShim{Relation: r, st: s.State}, nil
}

func (s stateShim) SaveEgressNetworks(relationKey string, cidrs []string) (state.RelationNetworks, error) {
	api := state.NewRelationEgressNetworks(s.State)
	return api.Save(relationKey, false, cidrs)
}

func (s stateShim) Model() (Model, error) {
	m, err := s.State.Model()
	if err != nil {
		return nil, err
	}
	return modelShim{Model: m}, nil
}

func (s stateShim) Relation(id int) (Relation, error) {
	r, err := s.State.Relation(id)
	if err != nil {
		return nil, err
	}
	return stateRelationShim{Relation: r, st: s.State}, nil
}

func (s stateShim) InferActiveRelation(names ...string) (Relation, error) {
	r, err := s.State.InferActiveRelation(names...)
	if err != nil {
		return nil, err
	}
	return stateRelationShim{Relation: r, st: s.State}, nil
}

func (s stateShim) Machine(name string) (Machine, error) {
	m, err := s.State.Machine(name)
	if err != nil {
		return nil, err
	}
	return stateMachineShim{Machine: m}, nil
}

func (s stateShim) Unit(name string) (Unit, error) {
	u, err := s.State.Unit(name)
	if err != nil {
		return nil, err
	}
	return stateUnitShim{
		Unit: u,
		st:   s.State,
	}, nil
}

func (s stateShim) UnitsInError() ([]Unit, error) {
	units, err := s.State.UnitsInError()
	if err != nil {
		return nil, err
	}
	result := make([]Unit, len(units))
	for i, u := range units {
		result[i] = stateUnitShim{
			Unit: u,
			st:   s.State,
		}
	}
	return result, nil
}

type OfferConnection interface {
	UserName() string
	OfferUUID() string
}

func (s stateShim) OfferConnectionForRelation(key string) (OfferConnection, error) {
	return s.State.OfferConnectionForRelation(key)
}

func (s stateShim) ApplicationOfferForUUID(offerUUID string) (*crossmodel.ApplicationOffer, error) {
	offers := state.NewApplicationOffers(s.State)
	return offers.ApplicationOfferForUUID(offerUUID)
}

type stateApplicationShim struct {
	*state.Application
	st *state.State
}

func (a stateApplicationShim) AddUnit(args state.AddUnitParams) (Unit, error) {
	u, err := a.Application.AddUnit(args)
	if err != nil {
		return nil, err
	}
	return stateUnitShim{
		Unit: u,
		st:   a.st,
	}, nil
}

func (a stateApplicationShim) AllUnits() ([]Unit, error) {
	units, err := a.Application.AllUnits()
	if err != nil {
		return nil, err
	}
	out := make([]Unit, len(units))
	for i, u := range units {
		out[i] = stateUnitShim{
			Unit: u,
			st:   a.st,
		}
	}
	return out, nil
}

func (a stateApplicationShim) Relations() ([]Relation, error) {
	rels, err := a.Application.Relations()
	if err != nil {
		return nil, err
	}
	out := make([]Relation, len(rels))
	for i, r := range rels {
		out[i] = stateRelationShim{Relation: r, st: a.st}
	}
	return out, nil
}

func (a stateApplicationShim) EndpointBindings() (Bindings, error) {
	return a.Application.EndpointBindings()
}

func (a stateApplicationShim) SetCharm(
	config state.SetCharmConfig,
	objStore objectstore.ObjectStore,
) error {
	return a.Application.SetCharm(config, objStore)
}

type stateMachineShim struct {
	*state.Machine
}

type stateRelationShim struct {
	*state.Relation
	st *state.State
}

func (r stateRelationShim) Unit(unitName string) (RelationUnit, error) {
	u, err := r.st.Unit(unitName)
	if err != nil {
		return nil, errors.Trace(err)
	}
	ru, err := r.Relation.Unit(u)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return stateRelationUnitShim{RelationUnit: ru}, nil
}

func (r stateRelationShim) AllRemoteUnits(appName string) ([]RelationUnit, error) {
	rus, err := r.Relation.AllRemoteUnits(appName)
	if err != nil {
		return nil, err
	}
	out := make([]RelationUnit, len(rus))
	for i, ru := range rus {
		out[i] = stateRelationUnitShim{RelationUnit: ru}
	}
	return out, nil
}

type stateRelationUnitShim struct {
	*state.RelationUnit
}

func (ru stateRelationUnitShim) Settings() (map[string]interface{}, error) {
	s, err := ru.RelationUnit.Settings()
	if err != nil {
		return nil, errors.Trace(err)
	}
	return s.Map(), nil
}

type stateUnitShim struct {
	*state.Unit
	st *state.State
}

func (u stateUnitShim) AssignUnit() error {
	return u.st.AssignUnit(u.Unit)
}

func (u stateUnitShim) AssignWithPlacement(placement *instance.Placement, allSpaces network.SpaceInfos) error {
	return u.st.AssignUnitWithPlacement(u.Unit, placement, allSpaces)
}

type Subnet interface {
	CIDR() string
	VLANTag() int
	ProviderId() network.Id
	ProviderNetworkId() network.Id
}
