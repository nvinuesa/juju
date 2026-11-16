// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agentrecovery

import (
	"context"

	"github.com/juju/juju/agent"
	coredatabase "github.com/juju/juju/core/database"
	"github.com/juju/juju/core/logger"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/user"
	accessrecovery "github.com/juju/juju/domain/access/recovery"
	controllernodeservice "github.com/juju/juju/domain/controllernode/service"
	controllernodestate "github.com/juju/juju/domain/controllernode/state"
	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/domain/schema"
	"github.com/juju/juju/domain/services"
	"github.com/juju/juju/internal/auth"
	"github.com/juju/juju/internal/database"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/recovery"
)

// Initialise opens the replacement controller database, adds the controller
// node row and replays the archived database dumps. No normal workers may own
// the data directory while this function runs.
func Initialise(
	ctx context.Context,
	config agent.Config,
	params recovery.Params,
	addresses network.ProviderAddresses,
	bindAddress string,
	log logger.Logger,
) error {
	certs, ok := config.ControllerAgentInfo()
	if !ok {
		return errors.New("missing controller agent information")
	}
	mgr := database.NewNodeManager(database.NodeManagerConfig{
		DataDir:              config.DataDir(),
		CACert:               config.CACert(),
		ControllerCert:       certs.Cert,
		ControllerPrivateKey: certs.PrivateKey,
	}, log, coredatabase.NoopSlowQueryLogger{})

	return database.WithDqlite(ctx, mgr, bindAddress, log, func(ctx context.Context, session *database.DqliteSession) error {
		_, controllerRunner, err := session.OpenDatabaseForImport(
			ctx, coredatabase.ControllerNS, schema.ControllerDDL(),
		)
		if err != nil {
			return errors.Capture(err)
		}
		nodes := controllernodeservice.NewService(
			controllernodestate.NewState(func(context.Context) (coredatabase.TxnRunner, error) {
				return controllerRunner, nil
			}),
			log,
		)
		if err := nodes.AddDqliteNode(
			ctx, params.MachineName, session.NodeID(), session.BindAddress(),
		); err != nil {
			return errors.Capture(err)
		}

		svc, err := services.NewRecoveryService(
			func(context.Context) (coredatabase.TxnRunner, error) {
				return controllerRunner, nil
			},
			func(ctx context.Context, modelUUID string) (coredatabase.TxnRunner, error) {
				return session.OpenDatabase(ctx, modelUUID, schema.ModelDDL())
			},
			params.MachineName,
		)
		if err != nil {
			return errors.Capture(err)
		}

		var machinePatch *domainrecovery.MachinePatch
		var unitAddresses []string
		if params.Node.BootstrapMachineInstanceId != "" {
			machinePatch = &domainrecovery.MachinePatch{
				MachineName: params.MachineName,
				InstanceID:  string(params.Node.BootstrapMachineInstanceId),
				DisplayName: params.Node.BootstrapMachineDisplayName,
				Nonce:       config.Nonce(),
			}
			if hw := params.Node.BootstrapMachineHardwareCharacteristics; hw != nil {
				if hw.Arch != nil {
					machinePatch.Arch = *hw.Arch
				}
				if hw.Mem != nil {
					machinePatch.MemMB = *hw.Mem
				}
				if hw.CpuCores != nil {
					machinePatch.Cores = *hw.CpuCores
				}
				if hw.RootDisk != nil {
					machinePatch.RootDiskMB = *hw.RootDisk
				}
			}
		} else {
			unitAddresses = addresses.Values()
		}
		var credentialPatch *recovery.CredentialPatch
		if credential := params.Node.ControllerCloudCredential; credential != nil {
			credentialPatch = &recovery.CredentialPatch{
				AuthType:   string(credential.AuthType()),
				Attributes: credential.Attributes(),
			}
		}

		if err := recovery.Load(ctx, recovery.LoadParams{
			Service:                 svc,
			ControllerID:            params.MachineName,
			ControllerCredential:    credentialPatch,
			ArchivePath:             params.ArchivePath,
			ExpectedSHA256:          params.SHA256,
			DataDir:                 config.DataDir(),
			ControllerModelUUID:     config.Model().Id(),
			ControllerUnitAddresses: unitAddresses,
			MachinePatch:            machinePatch,
			Logger:                  log,
		}); err != nil {
			return errors.Capture(err)
		}

		// Only the recovery client admin password is rebound. Other users
		// retain their archived credentials and permissions.
		return errors.Capture(accessrecovery.SetUserPassword(
			ctx, controllerRunner, user.AdminUserName, auth.NewPassword(config.OldPassword()),
		))
	})
}
