// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package factory

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names/v5"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/version/v2"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/environschema.v1"

	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/arch"
	corecharm "github.com/juju/juju/core/charm"
	coreconfig "github.com/juju/juju/core/config"
	"github.com/juju/juju/core/constraints"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/objectstore"
	"github.com/juju/juju/core/status"
	jujuversion "github.com/juju/juju/core/version"
	applicationerrors "github.com/juju/juju/domain/application/errors"
	applicationservice "github.com/juju/juju/domain/application/service"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/internal/charm"
	charmresource "github.com/juju/juju/internal/charm/resource"
	loggertesting "github.com/juju/juju/internal/logger/testing"
	internalobjectstore "github.com/juju/juju/internal/objectstore"
	objectstoretesting "github.com/juju/juju/internal/objectstore/testing"
	"github.com/juju/juju/internal/password"
	"github.com/juju/juju/internal/storage"
	"github.com/juju/juju/internal/storage/provider"
	"github.com/juju/juju/internal/testing"
	"github.com/juju/juju/internal/uuid"
	"github.com/juju/juju/state"
	"github.com/juju/juju/testcharms"
)

const (
	symbols = "abcdefghijklmopqrstuvwxyz"
)

type Factory struct {
	pool               *state.StatePool
	st                 *state.State
	applicationService *applicationservice.WatchableService
	modelConfigService state.ModelConfigService
	controllerConfig   controller.Config
}

var index uint32

func NewFactory(st *state.State, pool *state.StatePool, controllerConfig controller.Config) *Factory {
	return &Factory{
		st:               st,
		pool:             pool,
		controllerConfig: controllerConfig,
	}
}

// WithApplicationService configures the factory to use the specified application service.
func (f *Factory) WithApplicationService(s *applicationservice.WatchableService) *Factory {
	f.applicationService = s
	return f
}

// WithModelConfigService configures the factory to use the specified model config service.
func (f *Factory) WithModelConfigService(s state.ModelConfigService) *Factory {
	f.modelConfigService = s
	return f
}

// CharmParams defines the parameters for creating a charm.
type CharmParams struct {
	Name         string
	Series       string
	Revision     string
	Architecture string
	URL          string
}

// MachineParams are for creating a machine.
type MachineParams struct {
	Base            state.Base
	Jobs            []state.MachineJob
	Password        string
	Nonce           string
	Constraints     constraints.Value
	InstanceId      instance.Id
	DisplayName     string
	Characteristics *instance.HardwareCharacteristics
	Addresses       network.SpaceAddresses
	Volumes         []state.HostVolumeParams
	Filesystems     []state.HostFilesystemParams
}

// ApplicationParams is used when specifying parameters for a new application.
type ApplicationParams struct {
	Name                    string
	Charm                   *state.Charm
	CharmOrigin             *state.CharmOrigin
	Status                  *status.StatusInfo
	ApplicationConfig       map[string]interface{}
	ApplicationConfigFields environschema.Fields
	CharmConfig             map[string]interface{}
	Storage                 map[string]state.StorageConstraints
	Constraints             constraints.Value
	EndpointBindings        map[string]string
	Password                string
	Placement               []*instance.Placement
	DesiredScale            int
}

// UnitParams are used to create units.
type UnitParams struct {
	Application *state.Application
	Machine     *state.Machine
	Password    string
	SetCharmURL bool
	Status      *status.StatusInfo
	Constraints constraints.Value
}

// RelationParams are used to create relations.
type RelationParams struct {
	Endpoints []state.Endpoint
}

type ModelParams struct {
	Type                    state.ModelType
	UUID                    *uuid.UUID
	Name                    string
	Owner                   names.Tag
	ConfigAttrs             testing.Attrs
	CloudName               string
	CloudRegion             string
	CloudCredential         names.CloudCredentialTag
	StorageProviderRegistry storage.ProviderRegistry
	EnvironVersion          int
}

type SpaceParams struct {
	Name       string
	ProviderID network.Id
	SubnetIDs  []string
}

// RandomSuffix adds a random 5 character suffix to the presented string.
func (*Factory) RandomSuffix(prefix string) string {
	result := prefix
	for i := 0; i < 5; i++ {
		result += string(symbols[rand.Intn(len(symbols))])
	}
	return result
}

func uniqueInteger() int {
	return int(atomic.AddUint32(&index, 1))
}

func uniqueString(prefix string) string {
	if prefix == "" {
		prefix = "no-prefix"
	}
	return fmt.Sprintf("%s-%d", prefix, uniqueInteger())
}

func (factory *Factory) paramsFillDefaults(c *gc.C, params *MachineParams) *MachineParams {
	if params == nil {
		params = &MachineParams{}
	}
	if params.Base.String() == "" {
		params.Base = state.UbuntuBase("12.10")
	}
	if params.Nonce == "" {
		params.Nonce = "nonce"
	}
	if len(params.Jobs) == 0 {
		params.Jobs = []state.MachineJob{state.JobHostUnits}
	}
	if params.InstanceId == "" {
		params.InstanceId = instance.Id(uniqueString("id"))
	}
	if params.Password == "" {
		var err error
		params.Password, err = password.RandomPassword()
		c.Assert(err, jc.ErrorIsNil)
	}
	if params.Characteristics == nil {
		arch := arch.DefaultArchitecture
		mem := uint64(64 * 1024 * 1024 * 1024)
		hardware := instance.HardwareCharacteristics{
			Arch: &arch,
			Mem:  &mem,
		}
		params.Characteristics = &hardware
	}

	return params
}

// MakeMachineNested will make a machine nested in the machine with ID given.
func (factory *Factory) MakeMachineNested(c *gc.C, parentId string, params *MachineParams) *state.Machine {
	params = factory.paramsFillDefaults(c, params)
	machineTemplate := state.MachineTemplate{
		Base:        params.Base,
		Jobs:        params.Jobs,
		Volumes:     params.Volumes,
		Filesystems: params.Filesystems,
		Constraints: params.Constraints,
	}

	m, err := factory.st.AddMachineInsideMachine(
		factory.modelConfigService,
		machineTemplate,
		parentId,
		instance.LXD,
	)
	c.Assert(err, jc.ErrorIsNil)
	err = m.SetProvisioned(params.InstanceId, params.DisplayName, params.Nonce, params.Characteristics)
	c.Assert(err, jc.ErrorIsNil)
	current := testing.CurrentVersion()
	err = m.SetAgentVersion(current)
	c.Assert(err, jc.ErrorIsNil)
	return m
}

// MakeMachine will add a machine with values defined in params. For some
// values in params, if they are missing, some meaningful empty values will be
// set.
// If params is not specified, defaults are used.
func (factory *Factory) MakeMachine(c *gc.C, params *MachineParams) *state.Machine {
	machine, _ := factory.MakeMachineReturningPassword(c, params)
	return machine
}

// MakeMachineReturningPassword will add a machine with values defined in
// params. For some values in params, if they are missing, some meaningful
// empty values will be set. If params is not specified, defaults are used.
// The machine and its password are returned.
func (factory *Factory) MakeMachineReturningPassword(c *gc.C, params *MachineParams) (*state.Machine, string) {
	params = factory.paramsFillDefaults(c, params)
	return factory.makeMachineReturningPassword(c, params, true)
}

// MakeUnprovisionedMachineReturningPassword will add a machine with values
// defined in params. For some values in params, if they are missing, some
// meaningful empty values will be set. If params is not specified, defaults
// are used. The machine and its password are returned; the machine will not
// be provisioned.
func (factory *Factory) MakeUnprovisionedMachineReturningPassword(c *gc.C, params *MachineParams) (*state.Machine, string) {
	if params != nil {
		c.Assert(params.Nonce, gc.Equals, "")
		c.Assert(params.InstanceId, gc.Equals, instance.Id(""))
		c.Assert(params.Characteristics, gc.IsNil)
	}
	params = factory.paramsFillDefaults(c, params)
	params.Nonce = ""
	params.InstanceId = ""
	params.Characteristics = nil
	return factory.makeMachineReturningPassword(c, params, false)
}

func (factory *Factory) makeMachineReturningPassword(c *gc.C, params *MachineParams, setProvisioned bool) (*state.Machine, string) {
	machineTemplate := state.MachineTemplate{
		Base:        params.Base,
		Jobs:        params.Jobs,
		Volumes:     params.Volumes,
		Filesystems: params.Filesystems,
		Constraints: params.Constraints,
	}

	if params.Characteristics != nil {
		machineTemplate.HardwareCharacteristics = *params.Characteristics
	}
	machine, err := factory.st.AddOneMachine(factory.modelConfigService, machineTemplate)
	c.Assert(err, jc.ErrorIsNil)
	if setProvisioned {
		err = machine.SetProvisioned(params.InstanceId, params.DisplayName, params.Nonce, params.Characteristics)
		c.Assert(err, jc.ErrorIsNil)
	}
	err = machine.SetPassword(params.Password)
	c.Assert(err, jc.ErrorIsNil)
	if len(params.Addresses) > 0 {
		err = machine.SetProviderAddresses(factory.controllerConfig, params.Addresses...)
		c.Assert(err, jc.ErrorIsNil)
	}
	current := testing.CurrentVersion()
	err = machine.SetAgentVersion(current)
	c.Assert(err, jc.ErrorIsNil)
	return machine, params.Password
}

// MakeCharm creates a charm with the values specified in params.
// Sensible default values are substituted for missing ones.
// Supported charms depend on the charm/testing package.
// Currently supported charms:
//
//	all-hooks, category, dummy, logging, monitoring, mysql,
//	mysql-alternative, riak, terracotta, upgrade1, upgrade2, varnish,
//	varnish-alternative, wordpress.
//
// If params is not specified, defaults are used.
func (factory *Factory) MakeCharm(c *gc.C, params *CharmParams) *state.Charm {
	if params == nil {
		params = &CharmParams{}
	}
	model, err := factory.st.Model()
	c.Assert(err, jc.ErrorIsNil)
	if params.Name == "" {
		if model.Type() == state.ModelTypeCAAS {
			params.Name = "mysql-k8s"
		} else {
			params.Name = "mysql"
		}
	}
	if params.Series == "" {
		if model.Type() == state.ModelTypeCAAS {
			params.Series = "focal"
		} else {
			params.Series = "quantal"
		}
	}
	if params.Revision == "" {
		params.Revision = fmt.Sprintf("%d", uniqueInteger())
	}
	if params.URL == "" {
		params.URL = fmt.Sprintf("ch:amd64/%s/%s-%s", params.Series, params.Name, params.Revision)
	}

	ch := testcharms.RepoForSeries(params.Series).CharmDir(params.Name)

	bundleSHA256 := uniqueString("bundlesha")
	info := state.CharmInfo{
		Charm:       ch,
		ID:          params.URL,
		StoragePath: "fake-storage-path",
		SHA256:      bundleSHA256,
	}
	charm, err := factory.st.AddCharm(info)
	c.Assert(err, jc.ErrorIsNil)
	return charm
}

func (factory *Factory) MakeCharmV2(c *gc.C, params *CharmParams) *state.Charm {
	if params == nil {
		params = &CharmParams{}
	}
	if params.Name == "" {
		params.Name = "snappass-test"
	}
	if params.Series == "" {
		params.Series = "quantal"
	}
	if params.Architecture == "" {
		params.Architecture = "amd64"
	}
	if params.Revision == "" {
		params.Revision = fmt.Sprintf("%d", uniqueInteger())
	}
	if params.URL == "" {
		params.URL = fmt.Sprintf("ch:%s/%s/%s-%s", params.Architecture, params.Series, params.Name, params.Revision)
	}

	ch := testcharms.Hub.CharmDir(params.Name)

	bundleSHA256 := uniqueString("bundlesha")
	info := state.CharmInfo{
		Charm:       ch,
		ID:          params.URL,
		StoragePath: "fake-storage-path",
		SHA256:      bundleSHA256,
	}
	charm, err := factory.st.AddCharm(info)
	c.Assert(err, jc.ErrorIsNil)
	return charm
}

// MakeApplication creates an application with the specified parameters, substituting
// sane defaults for missing values.
// If params is not specified, defaults are used.
func (factory *Factory) MakeApplication(c *gc.C, params *ApplicationParams) *state.Application {
	app, _ := factory.MakeApplicationReturningPassword(c, params)
	return app
}

// MakeApplicationReturningPassword creates an application with the specified parameters, substituting
// sane defaults for missing values.
// If params is not specified, defaults are used.
// It returns the application and its password.
func (factory *Factory) MakeApplicationReturningPassword(c *gc.C, params *ApplicationParams) (*state.Application, string) {
	if params == nil {
		params = &ApplicationParams{}
	}
	if params.Charm == nil {
		params.Charm = factory.MakeCharm(c, nil)
	}
	if params.Name == "" {
		params.Name = params.Charm.Meta().Name
	}
	if params.Password == "" {
		var err error
		params.Password, err = password.RandomPassword()
		c.Assert(err, jc.ErrorIsNil)
	}
	if params.CharmOrigin == nil {
		curl := charm.MustParseURL(params.Charm.URL())
		var channel *state.Channel
		var source string
		// local charms cannot have a channel
		if charm.CharmHub.Matches(curl.Schema) {
			channel = &state.Channel{Risk: "stable"}
			source = "charm-hub"
		} else if charm.Local.Matches(curl.Schema) {
			source = "local"
		}
		params.CharmOrigin = &state.CharmOrigin{
			Channel: channel,
			Source:  source,
			Platform: &state.Platform{
				Architecture: curl.Architecture,
				OS:           "ubuntu",
				Channel:      "12.10",
			}}
	}

	objectStore := NewObjectStore(c,
		factory.st.ModelUUID(),
		objectstoretesting.MemoryMetadataService(),
		objectstoretesting.MemoryClaimer(),
	)
	rSt := factory.st.Resources(objectStore)

	resourceMap := make(map[string]string)
	for name, res := range params.Charm.Meta().Resources {
		pendingID, err := rSt.AddPendingResource(params.Name, "", charmresource.Resource{
			Meta:   res,
			Origin: charmresource.OriginUpload,
		})
		c.Assert(err, jc.ErrorIsNil)
		resourceMap[name] = pendingID
	}

	appConfig, err := coreconfig.NewConfig(params.ApplicationConfig, params.ApplicationConfigFields, nil)
	c.Assert(err, jc.ErrorIsNil)
	application, err := factory.st.AddApplication(
		factory.modelConfigService,
		state.AddApplicationArgs{
			Name:              params.Name,
			Charm:             params.Charm,
			CharmOrigin:       params.CharmOrigin,
			CharmConfig:       params.CharmConfig,
			ApplicationConfig: appConfig,
			Storage:           params.Storage,
			Constraints:       params.Constraints,
			Resources:         resourceMap,
			EndpointBindings:  params.EndpointBindings,
			Placement:         params.Placement,
		},
		objectStore,
	)
	c.Assert(err, jc.ErrorIsNil)
	err = application.SetPassword(params.Password)
	c.Assert(err, jc.ErrorIsNil)

	ch, _, err := application.Charm()
	c.Assert(err, jc.ErrorIsNil)

	if factory.applicationService != nil {
		directives := make(map[string]storage.Directive)
		for name, sc := range params.Storage {
			directives[name] = storage.Directive{
				Pool:  sc.Pool,
				Size:  sc.Size,
				Count: sc.Count,
			}
		}
		var channel *charm.Channel
		if params.CharmOrigin.Channel != nil {
			channel = &charm.Channel{
				Track:  params.CharmOrigin.Channel.Track,
				Risk:   charm.Risk(params.CharmOrigin.Channel.Risk),
				Branch: params.CharmOrigin.Channel.Branch,
			}
		}

		revision := ch.Revision()
		_, err = factory.applicationService.CreateApplication(context.Background(), params.Name, params.Charm, corecharm.Origin{
			Source:   corecharm.Source(params.CharmOrigin.Source),
			Type:     params.CharmOrigin.Type,
			ID:       params.CharmOrigin.ID,
			Hash:     params.CharmOrigin.Hash,
			Revision: &revision,
			Channel:  channel,
			Platform: corecharm.Platform{
				Architecture: params.CharmOrigin.Platform.Architecture,
				OS:           params.CharmOrigin.Platform.OS,
				Channel:      params.CharmOrigin.Platform.Channel,
			},
		}, applicationservice.AddApplicationArgs{
			ReferenceName: params.Name,
			Storage:       directives,
		})
	}
	c.Assert(err, jc.ErrorIsNil)

	model, err := factory.st.Model()
	c.Assert(err, jc.ErrorIsNil)
	isCAAS := model.Type() == state.ModelTypeCAAS
	if isCAAS {
		err = application.SetScale(params.DesiredScale, 0, true)
		c.Assert(err, jc.ErrorIsNil)
	}

	if params.Status != nil {
		now := time.Now()
		s := status.StatusInfo{
			Status:  params.Status.Status,
			Message: params.Status.Message,
			Data:    params.Status.Data,
			Since:   &now,
		}
		err = application.SetStatus(s)
		c.Assert(err, jc.ErrorIsNil)
	}

	if isCAAS {
		agentTools := version.Binary{
			Number:  jujuversion.Current,
			Arch:    arch.HostArch(),
			Release: application.CharmOrigin().Platform.OS,
		}
		err = application.SetAgentVersion(agentTools)
		c.Assert(err, jc.ErrorIsNil)
	}

	return application, params.Password
}

// MakeUnit creates an application unit with specified params, filling in
// sane defaults for missing values. If params is not specified, defaults
// are used.
//
// If the unit is being added to an IAAS model, then it will be assigned
// to a machine.
func (factory *Factory) MakeUnit(c *gc.C, params *UnitParams) *state.Unit {
	unit, _ := factory.MakeUnitReturningPassword(c, params)
	return unit
}

// MakeUnitReturningPassword creates an application unit with specified params,
// filling in sane defaults for missing values. If params is not specified,
// defaults are used. The unit and its password are returned.
//
// If the unit is being added to an IAAS model, then it will be assigned to a
// machine.
func (factory *Factory) MakeUnitReturningPassword(c *gc.C, params *UnitParams) (*state.Unit, string) {
	if params == nil {
		params = &UnitParams{}
	}
	model, err := factory.st.Model()
	c.Assert(err, jc.ErrorIsNil)
	switch model.Type() {
	case state.ModelTypeIAAS:
		if params.Machine == nil {
			var mParams *MachineParams
			if params.Application != nil {
				platform := params.Application.CharmOrigin().Platform
				mParams = &MachineParams{
					Base: state.Base{OS: platform.OS, Channel: platform.Channel},
				}
			}
			params.Machine = factory.MakeMachine(c, mParams)
		}
	default:
		if params.Machine != nil {
			c.Fatalf("machines not supported by model of type %q", model.Type())
		}
	}
	if params.Application == nil {
		params.Application = factory.MakeApplication(c, &ApplicationParams{
			Constraints: params.Constraints,
		})
	}
	if factory.applicationService != nil {
		chOrigin := params.Application.CharmOrigin()
		cons, err := params.Application.StorageConstraints()
		c.Assert(err, jc.ErrorIsNil)
		directives := make(map[string]storage.Directive)
		for k, v := range cons {
			directives[k] = storage.Directive{
				Pool:  v.Pool,
				Size:  v.Size,
				Count: v.Count,
			}
		}
		ch, _, err := params.Application.Charm()
		c.Assert(err, jc.ErrorIsNil)
		_, err = factory.applicationService.CreateApplication(
			context.Background(), params.Application.Name(),
			charm.NewCharmBase(ch.Meta(), ch.Manifest(), ch.Config(), ch.Actions(), ch.LXDProfile()),
			chOrigin.AsCoreCharmOrigin(), applicationservice.AddApplicationArgs{
				ReferenceName: params.Application.Name(),
				Storage:       directives,
			})
		if !errors.Is(err, applicationerrors.ApplicationAlreadyExists) {
			c.Assert(err, jc.ErrorIsNil)
		}
	}
	if params.Password == "" {
		var err error
		params.Password, err = password.RandomPassword()
		c.Assert(err, jc.ErrorIsNil)
	}
	unit, err := params.Application.AddUnit(factory.modelConfigService, state.AddUnitParams{})
	c.Assert(err, jc.ErrorIsNil)
	if factory.applicationService != nil {
		err = factory.applicationService.AddUnits(context.Background(), params.Application.Name(), applicationservice.AddUnitArg{
			UnitName: unit.Name(),
		})
		c.Assert(err, jc.ErrorIsNil)
	}

	if params.Machine != nil {
		err = unit.AssignToMachine(factory.modelConfigService, params.Machine)
		c.Assert(err, jc.ErrorIsNil)
	}

	agentTools := version.Binary{
		Number:  jujuversion.Current,
		Arch:    arch.HostArch(),
		Release: params.Application.CharmOrigin().Platform.OS,
	}
	err = unit.SetAgentVersion(agentTools)
	c.Assert(err, jc.ErrorIsNil)

	if params.SetCharmURL {
		applicationCharmURL, _ := params.Application.CharmURL()
		err = unit.SetCharmURL(*applicationCharmURL)
		c.Assert(err, jc.ErrorIsNil)
	}
	err = unit.SetPassword(params.Password)
	c.Assert(err, jc.ErrorIsNil)

	if params.Status != nil {
		now := time.Now()
		s := status.StatusInfo{
			Status:  params.Status.Status,
			Message: params.Status.Message,
			Data:    params.Status.Data,
			Since:   &now,
		}
		err = unit.SetStatus(s)
		c.Assert(err, jc.ErrorIsNil)
	}

	return unit, params.Password
}

// MakeRelation create a relation with specified params, filling in sane
// defaults for missing values.
// If params is not specified, defaults are used.
func (factory *Factory) MakeRelation(c *gc.C, params *RelationParams) *state.Relation {
	if params == nil {
		params = &RelationParams{}
	}
	if len(params.Endpoints) == 0 {
		s1 := factory.MakeApplication(c, &ApplicationParams{
			Charm: factory.MakeCharm(c, &CharmParams{
				Name: "mysql",
			}),
		})
		e1, err := s1.Endpoint("server")
		c.Assert(err, jc.ErrorIsNil)

		s2 := factory.MakeApplication(c, &ApplicationParams{
			Charm: factory.MakeCharm(c, &CharmParams{
				Name: "wordpress",
			}),
		})
		e2, err := s2.Endpoint("db")
		c.Assert(err, jc.ErrorIsNil)

		params.Endpoints = []state.Endpoint{e1, e2}
	}

	relation, err := factory.st.AddRelation(params.Endpoints...)
	c.Assert(err, jc.ErrorIsNil)

	return relation
}

// MakeModel creates an model with specified params,
// filling in sane defaults for missing values. If params is nil,
// defaults are used for all values.
//
// By default the new model shares the same owner as the calling Factory's
// model. TODO(ericclaudejones) MakeModel should return the model itself rather
// than the state.
func (factory *Factory) MakeModel(c *gc.C, params *ModelParams) *state.State {
	if params == nil {
		params = new(ModelParams)
	}
	if params.Type == state.ModelType("") {
		params.Type = state.ModelTypeIAAS
	}
	if params.Name == "" {
		params.Name = uniqueString("testmodel")
	}
	if params.CloudName == "" {
		params.CloudName = "dummy"
	}
	if params.CloudRegion == "" && params.CloudName == "dummy" {
		params.CloudRegion = "dummy-region"
	}
	if params.CloudRegion == "<none>" {
		params.CloudRegion = ""
	}
	if params.Owner == nil {
		origEnv, err := factory.st.Model()
		c.Assert(err, jc.ErrorIsNil)
		params.Owner = origEnv.Owner()
	}
	if params.StorageProviderRegistry == nil {
		params.StorageProviderRegistry = provider.CommonStorageProviders()
	}

	// For IAAS models, it only makes sense to make a model with the same provider
	// as the initial model, or things will break elsewhere.
	// For CAAS models, the type is "kubernetes".
	currentCfg := factory.currentCfg(c)
	cfgType := currentCfg.Type()
	if params.Type == state.ModelTypeCAAS {
		cfgType = "kubernetes"
	}

	var modelUUID uuid.UUID
	if params.UUID != nil {
		modelUUID = *params.UUID
	} else {
		modelUUID = uuid.MustNewUUID()
	}
	cfg := testing.CustomModelConfig(c, testing.Attrs{
		"name": params.Name,
		"uuid": modelUUID.String(),
		"type": cfgType,
	}.Merge(params.ConfigAttrs))
	controller := state.NewController(factory.pool)
	_, st, err := controller.NewModel(state.NoopConfigSchemaSource, state.ModelArgs{
		Type:                    params.Type,
		CloudName:               params.CloudName,
		CloudRegion:             params.CloudRegion,
		CloudCredential:         params.CloudCredential,
		Config:                  cfg,
		Owner:                   params.Owner.(names.UserTag),
		StorageProviderRegistry: params.StorageProviderRegistry,
		EnvironVersion:          params.EnvironVersion,
	})
	c.Assert(err, jc.ErrorIsNil)
	err = factory.pool.StartWorkers(st)
	c.Assert(err, jc.ErrorIsNil)
	return st
}

// MakeCAASModel creates a CAAS model with specified params,
// filling in sane defaults for missing values. If params is nil,
// defaults are used for all values.
func (factory *Factory) MakeCAASModel(c *gc.C, params *ModelParams) *state.State {
	if params == nil {
		params = &ModelParams{}
	}
	params.Type = state.ModelTypeCAAS
	if params.Owner == nil {
		origEnv, err := factory.st.Model()
		c.Assert(err, jc.ErrorIsNil)
		params.Owner = origEnv.Owner()
	}
	if params.CloudName == "" {
		params.CloudName = "caascloud"
	}
	if params.CloudCredential.IsZero() {
		if params.Owner == nil {
			origEnv, err := factory.st.Model()
			c.Assert(err, jc.ErrorIsNil)
			params.Owner = origEnv.Owner()
		}
		tag := names.NewCloudCredentialTag(
			fmt.Sprintf("%s/%s/dummy-credential", params.CloudName, params.Owner.Id()))
		params.CloudCredential = tag
	}
	return factory.MakeModel(c, params)
}

func (factory *Factory) currentCfg(c *gc.C) *config.Config {
	model, err := factory.st.Model()
	c.Assert(err, jc.ErrorIsNil)

	currentCfg, err := model.ModelConfig(context.Background())
	c.Assert(err, jc.ErrorIsNil)

	return currentCfg
}

func NewObjectStore(c *gc.C, modelUUID string, metadataService internalobjectstore.MetadataService, claimer internalobjectstore.Claimer) objectstore.ObjectStore {
	store, err := internalobjectstore.ObjectStoreFactory(
		context.Background(),
		internalobjectstore.DefaultBackendType(),
		modelUUID,
		internalobjectstore.WithRootDir(c.MkDir()),
		internalobjectstore.WithMetadataService(metadataService),
		internalobjectstore.WithClaimer(claimer),
		internalobjectstore.WithLogger(loggertesting.WrapCheckLog(c)),
	)
	c.Assert(err, jc.ErrorIsNil)
	return store
}