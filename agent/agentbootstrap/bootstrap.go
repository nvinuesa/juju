// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agentbootstrap

import (
	"context"
	"fmt"

	"github.com/juju/clock"
	"github.com/juju/errors"
	"github.com/juju/mgo/v3"
	"github.com/juju/names/v6"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/caas"
	"github.com/juju/juju/cloud"
	coreagent "github.com/juju/juju/core/agent"
	coreagentbinary "github.com/juju/juju/core/agentbinary"
	"github.com/juju/juju/core/credential"
	coredatabase "github.com/juju/juju/core/database"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/logger"
	coremodel "github.com/juju/juju/core/model"
	corenetwork "github.com/juju/juju/core/network"
	coreos "github.com/juju/juju/core/os"
	"github.com/juju/juju/core/permission"
	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/core/user"
	jujuversion "github.com/juju/juju/core/version"
	userbootstrap "github.com/juju/juju/domain/access/bootstrap"
	cloudbootstrap "github.com/juju/juju/domain/cloud/bootstrap"
	cloudimagemetadatabootstrap "github.com/juju/juju/domain/cloudimagemetadata/bootstrap"
	ccbootstrap "github.com/juju/juju/domain/controllerconfig/bootstrap"
	credbootstrap "github.com/juju/juju/domain/credential/bootstrap"
	modeldomain "github.com/juju/juju/domain/model"
	modelbootstrap "github.com/juju/juju/domain/model/bootstrap"
	modelerrors "github.com/juju/juju/domain/model/errors"
	modelconfigbootstrap "github.com/juju/juju/domain/modelconfig/bootstrap"
	modeldefaultsbootstrap "github.com/juju/juju/domain/modeldefaults/bootstrap"
	secretbackendbootstrap "github.com/juju/juju/domain/secretbackend/bootstrap"
	"github.com/juju/juju/environs"
	environscloudspec "github.com/juju/juju/environs/cloudspec"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/internal/auth"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/database"
	"github.com/juju/juju/internal/mongo"
	"github.com/juju/juju/internal/network"
	"github.com/juju/juju/internal/password"
	"github.com/juju/juju/internal/storage"
	"github.com/juju/juju/internal/uuid"
	"github.com/juju/juju/state"
)

// DqliteInitializerFunc is a function that initializes the dqlite database
// for the controller.
type DqliteInitializerFunc func(
	ctx context.Context,
	mgr database.BootstrapNodeManager,
	modelUUID coremodel.UUID,
	logger logger.Logger,
	options ...database.BootstrapOpt,
) error

type bootstrapController interface {
	state.Authenticator
	Id() string
	SetMongoPassword(password string) error
}

// AgentBootstrap is used to initialize the state for a new controller.
type AgentBootstrap struct {
	bootstrapEnviron environs.BootstrapEnviron
	adminUser        names.UserTag
	agentConfig      agent.ConfigSetter
	mongoDialOpts    mongo.DialOpts
	stateNewPolicy   state.NewPolicyFunc
	bootstrapDqlite  DqliteInitializerFunc

	stateInitializationParams instancecfg.StateInitializationParams
	// BootstrapMachineAddresses holds the bootstrap machine's addresses.
	bootstrapMachineAddresses corenetwork.ProviderAddresses

	// BootstrapMachineJobs holds the jobs that the bootstrap machine
	// agent will run.
	bootstrapMachineJobs []coremodel.MachineJob

	// SharedSecret is the Mongo replica set shared secret (keyfile).
	sharedSecret string

	// StorageProviderRegistry is used to determine and store the
	// details of the default storage pools.
	storageProviderRegistry storage.ProviderRegistry
	logger                  logger.Logger
}

// AgentBootstrapArgs are the arguments to NewAgentBootstrap that are required
// to NewAgentBootstrap.
type AgentBootstrapArgs struct {
	AdminUser                 names.UserTag
	AgentConfig               agent.ConfigSetter
	BootstrapEnviron          environs.BootstrapEnviron
	BootstrapMachineAddresses corenetwork.ProviderAddresses
	BootstrapMachineJobs      []coremodel.MachineJob
	MongoDialOpts             mongo.DialOpts
	SharedSecret              string
	StateInitializationParams instancecfg.StateInitializationParams
	StorageProviderRegistry   storage.ProviderRegistry
	BootstrapDqlite           DqliteInitializerFunc
	Logger                    logger.Logger
}

func (a *AgentBootstrapArgs) validate() error {
	if a.BootstrapEnviron == nil {
		return errors.NotValidf("bootstrap environ")
	}
	if a.AdminUser == (names.UserTag{}) {
		return errors.NotValidf("admin user")
	}
	if a.AgentConfig == nil {
		return errors.NotValidf("agent config")
	}
	if a.SharedSecret == "" {
		return errors.NotValidf("shared secret")
	}
	if a.StorageProviderRegistry == nil {
		return errors.NotValidf("storage provider registry")
	}
	if a.BootstrapDqlite == nil {
		return errors.NotValidf("bootstrap dqlite")
	}
	if a.Logger == nil {
		return errors.NotValidf("logger")
	}
	return nil
}

// NewAgentBootstrap creates a new AgentBootstrap, that can be used to
// initialize the state for a new controller.
// NewAgentBootstrap should be called with the bootstrap machine's agent
// configuration. It uses that information to create the controller, dial the
// controller, and initialize it. It also generates a new password for the
// bootstrap machine and calls Write to save the configuration.
//
// The cfg values will be stored in the state's ModelConfig; the
// machineCfg values will be used to configure the bootstrap Machine,
// and its constraints will be also be used for the model-level
// constraints. The connection to the controller will respect the
// given timeout parameter.
func NewAgentBootstrap(args AgentBootstrapArgs) (*AgentBootstrap, error) {
	if err := args.validate(); err != nil {
		return nil, errors.Trace(err)
	}
	return &AgentBootstrap{
		adminUser:                 args.AdminUser,
		agentConfig:               args.AgentConfig,
		bootstrapDqlite:           args.BootstrapDqlite,
		bootstrapEnviron:          args.BootstrapEnviron,
		bootstrapMachineAddresses: args.BootstrapMachineAddresses,
		bootstrapMachineJobs:      args.BootstrapMachineJobs,
		logger:                    args.Logger,
		mongoDialOpts:             args.MongoDialOpts,
		sharedSecret:              args.SharedSecret,
		stateInitializationParams: args.StateInitializationParams,
		storageProviderRegistry:   args.StorageProviderRegistry,
	}, nil
}

// Initialize returns the newly initialized state and bootstrap machine.
// If it fails, the state may well be irredeemably compromised.
// TODO (stickupkid): Split this function into testable smaller functions.
func (b *AgentBootstrap) Initialize(ctx context.Context) (_ *state.Controller, resultErr error) {
	agentConfig := b.agentConfig
	if agentConfig.Tag().Id() != agent.BootstrapControllerId || !coreagent.IsAllowedControllerTag(agentConfig.Tag().Kind()) {
		return nil, errors.Errorf("InitializeState not called with bootstrap controller's configuration")
	}
	servingInfo, ok := agentConfig.StateServingInfo()
	if !ok {
		return nil, errors.Errorf("state serving information not available")
	}
	// N.B. no users are set up when we're initializing the state,
	// so don't use any tag or password when opening it.
	info, ok := agentConfig.MongoInfo()
	if !ok {
		return nil, errors.Errorf("state info not available")
	}
	info.Tag = nil
	info.Password = agentConfig.OldPassword()

	stateParams := b.stateInitializationParams

	// Add the controller model cloud and credential to the database.
	cloudCred, cloudCredTag, err := b.getCloudCredential()
	if err != nil {
		return nil, errors.Annotate(err, "getting cloud credentials from args")
	}

	controllerUUID, err := uuid.UUIDFromString(stateParams.ControllerConfig.ControllerUUID())
	if err != nil {
		return nil, fmt.Errorf("parsing controller uuid %q: %w", stateParams.ControllerConfig.ControllerUUID(), err)
	}

	controllerModelUUID := coremodel.UUID(
		stateParams.ControllerModelConfig.UUID(),
	)

	// Add initial Admin user to the database. This will return Admin user UUID
	// and a function to insert it into the database.
	adminUserUUID, addAdminUser := userbootstrap.AddUserWithPassword(
		user.NameFromTag(b.adminUser),
		auth.NewPassword(info.Password),
		permission.AccessSpec{
			Access: permission.SuperuserAccess,
			Target: permission.ID{
				ObjectType: permission.Controller,
				Key:        controllerUUID.String(),
			},
		},
	)

	controllerModelArgs := modeldomain.GlobalModelCreationArgs{
		Name:        stateParams.ControllerModelConfig.Name(),
		Owner:       adminUserUUID,
		Cloud:       stateParams.ControllerCloud.Name,
		CloudRegion: stateParams.ControllerCloudRegion,
		Credential:  credential.KeyFromTag(cloudCredTag),
	}
	controllerModelCreateFunc := modelbootstrap.CreateGlobalModelRecord(controllerModelUUID, controllerModelArgs)

	controllerModelDefaults := modeldefaultsbootstrap.ModelDefaultsProvider(
		stateParams.ControllerInheritedConfig,
		stateParams.RegionInheritedConfig[stateParams.ControllerCloudRegion],
		stateParams.ControllerCloud.Type,
	)

	isCAAS := cloud.CloudIsCAAS(stateParams.ControllerCloud)
	modelType := state.ModelTypeIAAS
	if isCAAS {
		modelType = state.ModelTypeCAAS
	}

	agentVersion := stateParams.AgentVersion
	if agentVersion == semversion.Zero {
		agentVersion = jujuversion.Current
	}
	if agentVersion.Major != jujuversion.Current.Major || agentVersion.Minor != jujuversion.Current.Minor {
		return nil, fmt.Errorf("%w %q during bootstrap", modelerrors.AgentVersionNotSupported, agentVersion)
	}

	// localModelRecordOP defines the bootstrap operation that should be run
	// to establish the local model record in the controller model's database.
	// We have two variants of this to handle the case when the user as set a
	// custom agent stream to use for the controller model.
	localModelRecordOp := modelbootstrap.CreateLocalModelRecord(
		controllerModelUUID, controllerUUID, agentVersion,
	)
	if stateParams.ControllerModelConfig.AgentStream() != "" {
		agentStream := coreagentbinary.AgentStream(stateParams.ControllerModelConfig.AgentStream())
		localModelRecordOp = modelbootstrap.CreateLocalModelRecordWithAgentStream(
			controllerModelUUID, controllerUUID, agentVersion, agentStream,
		)
	}

	databaseBootstrapOptions := []database.BootstrapOpt{
		// The controller config needs to be inserted before the admin users
		// because the admin users permissions require the controller UUID.
		ccbootstrap.InsertInitialControllerConfig(stateParams.ControllerConfig, controllerModelUUID),
		// The admin user needs to be added before everything else that
		// requires being owned by a Juju user.
		addAdminUser,
		cloudbootstrap.InsertCloud(user.NameFromTag(b.adminUser), stateParams.ControllerCloud),
		credbootstrap.InsertCredential(credential.KeyFromTag(cloudCredTag), cloudCred),
		modeldefaultsbootstrap.SetCloudDefaults(stateParams.ControllerCloud.Name, stateParams.ControllerInheritedConfig),
		secretbackendbootstrap.CreateDefaultBackends(coremodel.ModelType(modelType)),
		controllerModelCreateFunc,
		localModelRecordOp,
		modelbootstrap.SetModelConstraints(stateParams.ModelConstraints),
		modelconfigbootstrap.SetModelConfig(
			controllerModelUUID, stateParams.ControllerModelConfig.AllAttrs(), controllerModelDefaults),
	}
	if !isCAAS {
		databaseBootstrapOptions = append(databaseBootstrapOptions,
			cloudimagemetadatabootstrap.AddCustomImageMetadata(
				clock.WallClock, stateParams.ControllerModelConfig.ImageStream(), stateParams.CustomImageMetadata),
		)
	}

	// If we're running caas, we need to bind to the loopback address
	// and eschew TLS termination.
	// This is to prevent dqlite to become all at sea when the controller pod
	// is rescheduled. This is only a temporary measure until we have HA
	// dqlite for k8s.
	isLoopbackPreferred := isCAAS

	if err := b.bootstrapDqlite(
		ctx,
		database.NewNodeManager(b.agentConfig, isLoopbackPreferred, b.logger, coredatabase.NoopSlowQueryLogger{}),
		controllerModelUUID,
		b.logger,
		databaseBootstrapOptions...,
	); err != nil {
		return nil, errors.Trace(err)
	}

	session, err := b.initMongo(info.Info, b.mongoDialOpts, info.Password)
	if err != nil {
		return nil, errors.Annotate(err, "failed to initialize mongo")
	}
	defer session.Close()

	b.logger.Debugf(context.TODO(), "initializing address %v", info.Addrs)

	ctrl, err := state.Initialize(state.InitializeParams{
		SSHServerHostKey: stateParams.SSHServerHostKey,
		Clock:            clock.WallClock,
		ControllerModelArgs: state.ModelArgs{
			Name:            stateParams.ControllerModelConfig.Name(),
			UUID:            coremodel.UUID(stateParams.ControllerModelConfig.UUID()),
			Type:            modelType,
			Owner:           b.adminUser,
			CloudName:       stateParams.ControllerCloud.Name,
			CloudRegion:     stateParams.ControllerCloudRegion,
			CloudCredential: cloudCredTag,
		},
		StoragePools:              stateParams.StoragePools,
		CloudName:                 stateParams.ControllerCloud.Name,
		ControllerConfig:          stateParams.ControllerConfig,
		ControllerInheritedConfig: stateParams.ControllerInheritedConfig,
		RegionInheritedConfig:     stateParams.RegionInheritedConfig,
		MongoSession:              session,
		AdminPassword:             info.Password,
		NewPolicy:                 b.stateNewPolicy,
	})
	if err != nil {
		return nil, errors.Errorf("failed to initialize state: %v", err)
	}
	b.logger.Debugf(context.TODO(), "connected to initial state")
	defer func() {
		if resultErr != nil {
			_ = ctrl.Close()
		}
	}()
	servingInfo.SharedSecret = b.sharedSecret
	b.agentConfig.SetStateServingInfo(servingInfo)

	// Filter out any LXC or LXD bridge addresses from the machine addresses.
	filteredBootstrapMachineAddresses := network.FilterBridgeAddresses(ctx, b.bootstrapMachineAddresses)

	st, err := ctrl.SystemState()
	if err != nil {
		return nil, errors.Trace(err)
	}

	if err := st.SetStateServingInfo(servingInfo); err != nil {
		return nil, errors.Errorf("cannot set state serving info: %v", err)
	}

	var controllerNode bootstrapController
	if isCAAS {
		if controllerNode, err = b.initBootstrapNode(st); err != nil {
			return nil, errors.Annotate(err, "cannot initialize bootstrap controller")
		}
	} else {
		if controllerNode, err = b.initBootstrapMachine(st, filteredBootstrapMachineAddresses); err != nil {
			return nil, errors.Annotate(err, "cannot initialize bootstrap machine")
		}
	}

	// Sanity check.
	if controllerNode.Id() != agent.BootstrapControllerId {
		return nil, errors.Errorf("bootstrap controller expected id 0, got %q", controllerNode.Id())
	}

	// Read the machine agent's password and change it to
	// a new password (other agents will change their password
	// via the API connection).
	b.logger.Debugf(context.TODO(), "create new random password for controller %v", controllerNode.Id())

	newPassword, err := password.RandomPassword()
	if err != nil {
		return nil, err
	}
	if err := controllerNode.SetPassword(newPassword); err != nil {
		return nil, err
	}
	if err := controllerNode.SetMongoPassword(newPassword); err != nil {
		return nil, err
	}
	b.agentConfig.SetPassword(newPassword)

	return ctrl, nil
}

func (b *AgentBootstrap) getCloudCredential() (cloud.Credential, names.CloudCredentialTag, error) {
	var cloudCredentialTag names.CloudCredentialTag

	stateParams := b.stateInitializationParams
	if stateParams.ControllerCloudCredential != nil && stateParams.ControllerCloudCredentialName != "" {
		id := fmt.Sprintf(
			"%s/%s/%s",
			stateParams.ControllerCloud.Name,
			b.adminUser.Id(),
			stateParams.ControllerCloudCredentialName,
		)
		if !names.IsValidCloudCredential(id) {
			return cloud.Credential{}, cloudCredentialTag, errors.NotValidf("cloud credential UUID %q", id)
		}
		cloudCredentialTag = names.NewCloudCredentialTag(id)
		return *stateParams.ControllerCloudCredential, cloudCredentialTag, nil
	}
	return cloud.Credential{}, cloudCredentialTag, nil
}

func (b *AgentBootstrap) getEnviron(
	ctx context.Context,
	controllerUUID string,
	cloudSpec environscloudspec.CloudSpec,
	modelConfig *config.Config,
	provider environs.EnvironProvider,
) (env environs.BootstrapEnviron, err error) {
	openParams := environs.OpenParams{
		ControllerUUID: controllerUUID,
		Cloud:          cloudSpec,
		Config:         modelConfig,
	}
	if cloud.CloudTypeIsCAAS(cloudSpec.Type) {
		return caas.Open(ctx, provider, openParams, environs.NoopCredentialInvalidator())
	}
	return environs.Open(ctx, provider, openParams, environs.NoopCredentialInvalidator())
}

// initMongo dials the initial MongoDB connection, setting a
// password for the admin user, and returning the session.
func (b *AgentBootstrap) initMongo(info mongo.Info, dialOpts mongo.DialOpts, password string) (*mgo.Session, error) {
	session, err := mongo.DialWithInfo(mongo.MongoInfo{Info: info}, dialOpts)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if err := mongo.SetAdminMongoPassword(session, mongo.AdminUser, password); err != nil {
		session.Close()
		return nil, errors.Trace(err)
	}
	if err := mongo.Login(session, mongo.AdminUser, password); err != nil {
		session.Close()
		return nil, errors.Trace(err)
	}
	return session, nil
}

// initBootstrapMachine initializes the initial bootstrap machine in state.
func (b *AgentBootstrap) initBootstrapMachine(
	st *state.State,
	bootstrapMachineAddresses corenetwork.ProviderAddresses,
) (bootstrapController, error) {
	stateParams := b.stateInitializationParams
	b.logger.Infof(context.TODO(), "initialising bootstrap machine with config: %+v", stateParams)

	jobs := make([]state.MachineJob, len(b.bootstrapMachineJobs))
	for i, job := range b.bootstrapMachineJobs {
		machineJob, err := machineJobFromParams(job)
		if err != nil {
			return nil, errors.Errorf("invalid bootstrap machine job %q: %v", job, err)
		}
		jobs[i] = machineJob
	}
	var hardware instance.HardwareCharacteristics
	if stateParams.BootstrapMachineHardwareCharacteristics != nil {
		hardware = *stateParams.BootstrapMachineHardwareCharacteristics
	}

	base, err := coreos.HostBase()
	if err != nil {
		return nil, errors.Trace(err)
	}

	// TODO: move this call to the bootstrap worker
	m, err := st.AddOneMachine(
		state.MachineTemplate{
			Base:                    state.Base{OS: base.OS, Channel: base.Channel.String()},
			Nonce:                   agent.BootstrapNonce,
			Constraints:             stateParams.BootstrapMachineConstraints,
			InstanceId:              stateParams.BootstrapMachineInstanceId,
			HardwareCharacteristics: hardware,
			Jobs:                    jobs,
			DisplayName:             stateParams.BootstrapMachineDisplayName,
		},
	)
	if err != nil {
		return nil, errors.Annotate(err, "cannot create bootstrap machine in state")
	}
	return m, nil
}

// initBootstrapNode initializes the initial caas bootstrap controller in state.
func (b *AgentBootstrap) initBootstrapNode(
	st *state.State,
) (bootstrapController, error) {
	b.logger.Debugf(context.TODO(), "initialising bootstrap node for with config: %+v", b.stateInitializationParams)

	node, err := st.AddControllerNode()
	if err != nil {
		return nil, errors.Annotate(err, "cannot create bootstrap controller in state")
	}
	return node, nil
}

// machineJobFromParams returns the job corresponding to model.MachineJob.
// TODO(dfc) this function should live in apiserver/params, move there once
// state does not depend on apiserver/params
func machineJobFromParams(job coremodel.MachineJob) (state.MachineJob, error) {
	switch job {
	case coremodel.JobHostUnits:
		return state.JobHostUnits, nil
	case coremodel.JobManageModel:
		return state.JobManageModel, nil
	default:
		return -1, errors.Errorf("invalid machine job %q", job)
	}
}
