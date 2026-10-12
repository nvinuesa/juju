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
	"github.com/juju/juju/core/version"
	accessrecovery "github.com/juju/juju/domain/access/recovery"
	controllernodeservice "github.com/juju/juju/domain/controllernode/service"
	controllernodestate "github.com/juju/juju/domain/controllernode/state"
	domainrecovery "github.com/juju/juju/domain/recovery"
	recoveryservice "github.com/juju/juju/domain/recovery/service"
	"github.com/juju/juju/domain/schema"
	"github.com/juju/juju/domain/services"
	"github.com/juju/juju/internal/auth"
	"github.com/juju/juju/internal/database"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/password"
	"github.com/juju/juju/internal/recovery"
)

// Initialise restores databases and updates the agent password only on success.
// No normal workers may own the data directory while this function runs.
func Initialise(ctx context.Context, config agent.ConfigSetter, params recovery.Params,
	addresses network.ProviderAddresses, log logger.Logger) error {
	info, err := recovery.ValidateArchiveIn(ctx, log, params.ArchivePath, params.SHA256, config.DataDir())
	if err != nil {
		return errors.Capture(err)
	}
	if info.AgentVersion != params.AgentVersion {
		return errors.New("recovery parameters disagree with archive version")
	}
	if err := recoveryservice.CheckAgentVersion(info, version.Current); err != nil {
		return errors.Capture(err)
	}
	if config.Controller().Id() != info.ControllerUUID || config.Model().Id() != info.ControllerModelUUID {
		return errors.New("recovery agent identity disagrees with archive")
	}
	certs, ok := config.ControllerAgentInfo()
	if !ok {
		return errors.New("missing controller agent information")
	}
	mgr := database.NewNodeManager(database.NodeManagerConfig{
		DataDir: config.DataDir(), CACert: config.CACert(),
		ControllerCert: certs.Cert, ControllerPrivateKey: certs.PrivateKey,
	}, log, coredatabase.NoopSlowQueryLogger{})
	return database.WithDqlite(ctx, mgr, addresses, log, func(ctx context.Context, session *database.DqliteSession) error {
		_, controllerRunner, err := session.OpenDatabaseForImport(ctx, coredatabase.ControllerNS, schema.ControllerDDL())
		if err != nil {
			return errors.Capture(err)
		}
		nodes := controllernodeservice.NewService(controllernodestate.NewState(
			func(context.Context) (coredatabase.TxnRunner, error) {
				return controllerRunner, nil
			}), log)
		if err := nodes.AddDqliteNode(ctx, params.MachineName, session.NodeID(), session.BindAddress()); err != nil {
			return errors.Capture(err)
		}

		var patch *domainrecovery.MachinePatch
		var unitAddresses []string
		if params.Node.BootstrapMachineInstanceId != "" {
			patch = &domainrecovery.MachinePatch{MachineName: params.MachineName, InstanceID: string(params.Node.BootstrapMachineInstanceId), DisplayName: params.Node.BootstrapMachineDisplayName, Nonce: params.MachineNonce}
			if hw := params.Node.BootstrapMachineHardwareCharacteristics; hw != nil {
				if hw.Arch != nil {
					patch.Arch = *hw.Arch
				}
				if hw.Mem != nil {
					patch.MemMB = *hw.Mem
				}
				if hw.CpuCores != nil {
					patch.Cores = *hw.CpuCores
				}
				if hw.RootDisk != nil {
					patch.RootDiskMB = *hw.RootDisk
				}
			}
		} else {
			unitAddresses = addresses.Values()
		}
		var credentialPatch *recovery.CredentialPatch
		if credential := params.Node.ControllerCloudCredential; credential != nil && info.Credential != nil {
			credentialPatch = &recovery.CredentialPatch{
				AuthType:   string(credential.AuthType()),
				Attributes: credential.Attributes(),
			}
		}
		svc, err := services.NewRecoveryService(
			func(context.Context) (coredatabase.TxnRunner, error) { return controllerRunner, nil },
			func(ctx context.Context, modelUUID string) (coredatabase.TxnRunner, error) {
				return session.OpenDatabase(ctx, modelUUID, schema.ModelDDL())
			}, params.MachineName)
		if err != nil {
			return errors.Capture(err)
		}
		_, err = recovery.Load(ctx, recovery.LoadParams{
			Service: svc, ControllerID: params.MachineName,
			Credentials: params.Credentials,
			ArchivePath: params.ArchivePath, ExpectedSHA256: params.SHA256,
			DataDir: config.DataDir(),
			ControllerModelUUID: info.ControllerModelUUID, MachinePatch: patch,
			ControllerUnitAddresses: unitAddresses, ControllerCredential: credentialPatch,
			Logger: log,
		})
		if err != nil {
			return errors.Capture(err)
		}
		// Only the recovery client admin password is rebound. Other users
		// retain their archived credentials and permissions.
		if err := accessrecovery.SetUserPassword(ctx, controllerRunner, user.AdminUserName, auth.NewPassword(config.OldPassword())); err != nil {
			return errors.Capture(err)
		}
		newPassword, err := password.RandomPassword()
		if err != nil {
			return errors.Capture(err)
		}
		config.SetPassword(newPassword)
		return nil
	})
}
