// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agentrecovery

import (
	"context"
	"database/sql"
	"path/filepath"

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
	recoverystate "github.com/juju/juju/domain/recovery/state"
	"github.com/juju/juju/domain/schema"
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
	info, err := recovery.ValidateArchive(ctx, params.ArchivePath, params.SHA256)
	if err != nil {
		return errors.Capture(err)
	}
	if info.AgentVersion != params.AgentVersion {
		return errors.New("recovery parameters disagree with archive version")
	}
	if err := info.CheckAgentVersion(version.Current); err != nil {
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
		controllerDB, controllerRunner, err := session.OpenDatabaseForImport(ctx, coredatabase.ControllerNS, schema.ControllerDDL())
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
		_, err = recovery.Load(ctx, recovery.LoadParams{
			ControllerDB: controllerDB,
			OpenModelDB: func(ctx context.Context, uuid string) (*sql.DB, error) {
				return session.OpenSQLDatabase(ctx, uuid, schema.ModelDDL())
			},
			ArchivePath: params.ArchivePath, ExpectedSHA256: params.SHA256,
			BundleDir: filepath.Join(config.DataDir(), "recovery", "bundle"), DataDir: config.DataDir(),
			ControllerModelUUID: info.ControllerModelUUID, MachinePatch: patch,
			ControllerUnitAddresses: unitAddresses, Logger: log,
		})
		if err != nil {
			return errors.Capture(err)
		}
		// Only the recovery client admin password is rebound. Other users
		// retain their archived credentials and permissions.
		if err := accessrecovery.SetUserPassword(ctx, controllerRunner, user.AdminUserName, auth.NewPassword(config.OldPassword())); err != nil {
			return errors.Capture(err)
		}
		if credential := params.Node.ControllerCloudCredential; credential != nil && info.Credential != nil {
			if err := recoverystate.PatchControllerCredential(ctx, controllerDB, info.ControllerModelUUID, string(credential.AuthType()), credential.Attributes()); err != nil {
				return errors.Capture(err)
			}
		}
		newPassword, err := password.RandomPassword()
		if err != nil {
			return errors.Capture(err)
		}
		config.SetPassword(newPassword)
		return nil
	})
}
