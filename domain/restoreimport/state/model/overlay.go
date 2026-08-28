// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package model

import (
	"context"

	"github.com/canonical/sqlair"

	"github.com/juju/juju/domain/export/types/v4_1_0"
	"github.com/juju/juju/internal/errors"
)

// ModelOverlay carries the target-local model facts captured before import.
// The importer must not make hidden target reads: every target value it
// writes is passed here explicitly.
type ModelOverlay struct {
	// NodeID is the controller node ID blobs are assigned to ("0"). The
	// restored object_store_placement rows describe source nodes; on a
	// single-node restore every placement is rewritten to this node.
	NodeID string

	// ModelAgentPasswordHash is the target model-agent password hash, merged
	// onto the restored model_agent row. Empty means do not touch the
	// restored hash (e.g. non-controller models keep the source hash).
	ModelAgentPasswordHash string

	// Machine0PasswordHash/AlgorithmID is the target machine-0 password
	// material, merged onto the restored machine row for the controller
	// model. Nil means do not touch the restored hash (non-controller
	// models keep the source hash).
	Machine0PasswordHash *string
	Machine0AlgorithmID  *string

	Machine0                  v4_1_0.Machine
	Machine0CloudInstance     *v4_1_0.MachineCloudInstance
	ControllerUnitNetNodeUUID string
	NetNodes                  []v4_1_0.NetNode
	LinkLayerDevices          []v4_1_0.LinkLayerDevice
	IPAddresses               []v4_1_0.IpAddress
}

// ApplyOverlay applies the target-local model facts onto the restored model
// database. The bulk import restored the source rows (including model_agent
// with the source hash); this phase then:
//
//   - rewrites every object_store_placement row to the single restore node;
//   - optionally merges the captured target password hash onto the model
//     agent row (controller-model only);
//   - clears the changelog and removes restored AUTOINCREMENT entries so
//     SQLite allocation starts after current maxima.
func (st *State) ApplyOverlay(ctx context.Context, p *v4_1_0.ModelExport, o ModelOverlay) error {
	db, err := st.DB(ctx)
	if err != nil {
		return errors.Errorf("getting db: %w", err)
	}

	stmtUpdate, err := sqlair.Prepare(`UPDATE "model_agent" SET password_hash = $ModelAgent.password_hash WHERE model_uuid = $ModelAgent.model_uuid`, v4_1_0.ModelAgent{})
	if err != nil {
		return errors.Errorf("preparing model_agent update statement: %w", err)
	}
	stmtUpdateMachine, err := sqlair.Prepare(`UPDATE "machine" SET password_hash = $Machine.password_hash, password_hash_algorithm_id = $Machine.password_hash_algorithm_id WHERE name = '0'`, v4_1_0.Machine{})
	if err != nil {
		return errors.Errorf("preparing machine password update statement: %w", err)
	}
	stmtInsertNetNode, err := sqlair.Prepare(`INSERT INTO "net_node" (*) VALUES ($NetNode.*) ON CONFLICT DO NOTHING`, v4_1_0.NetNode{})
	if err != nil {
		return errors.Errorf("preparing target net node insert statement: %w", err)
	}
	stmtInsertDevice, err := sqlair.Prepare(`INSERT INTO "link_layer_device" (*) VALUES ($LinkLayerDevice.*) ON CONFLICT DO NOTHING`, v4_1_0.LinkLayerDevice{})
	if err != nil {
		return errors.Errorf("preparing target link layer device insert statement: %w", err)
	}
	stmtInsertAddress, err := sqlair.Prepare(`INSERT INTO "ip_address" (*) VALUES ($IpAddress.*) ON CONFLICT DO NOTHING`, v4_1_0.IpAddress{})
	if err != nil {
		return errors.Errorf("preparing target IP address insert statement: %w", err)
	}
	stmtUpdateMachineIdentity, err := sqlair.Prepare(`UPDATE "machine" SET net_node_uuid = $Machine.net_node_uuid, nonce = $Machine.nonce, password_hash_algorithm_id = $Machine.password_hash_algorithm_id, password_hash = $Machine.password_hash, agent_started_at = $Machine.agent_started_at, hostname = $Machine.hostname, keep_instance = $Machine.keep_instance WHERE name = '0'`, v4_1_0.Machine{})
	if err != nil {
		return errors.Errorf("preparing machine identity update statement: %w", err)
	}
	stmtUpdateControllerUnit, err := sqlair.Prepare(`UPDATE "unit" SET net_node_uuid = $Unit.net_node_uuid WHERE name = 'controller/0'`, v4_1_0.Unit{})
	if err != nil {
		return errors.Errorf("preparing controller unit network update statement: %w", err)
	}
	stmtUpdateMachineInstance, err := sqlair.Prepare(`UPDATE "machine_cloud_instance" SET instance_id = $MachineCloudInstance.instance_id, display_name = $MachineCloudInstance.display_name, arch = $MachineCloudInstance.arch, cpu_cores = $MachineCloudInstance.cpu_cores, cpu_power = $MachineCloudInstance.cpu_power, mem = $MachineCloudInstance.mem, root_disk = $MachineCloudInstance.root_disk, root_disk_source = $MachineCloudInstance.root_disk_source, virt_type = $MachineCloudInstance.virt_type WHERE machine_uuid = $MachineCloudInstance.machine_uuid`, v4_1_0.MachineCloudInstance{})
	if err != nil {
		return errors.Errorf("preparing machine instance update statement: %w", err)
	}
	stmtClearChangeLog, err := sqlair.Prepare(`DELETE FROM "change_log"`)
	if err != nil {
		return errors.Errorf("preparing change_log clear statement: %w", err)
	}
	stmtClearChangeLogWitness, err := sqlair.Prepare(`DELETE FROM "change_log_witness"`)
	if err != nil {
		return errors.Errorf("preparing change_log_witness clear statement: %w", err)
	}
	stmtClearPlacement, err := sqlair.Prepare(`DELETE FROM "object_store_placement"`)
	if err != nil {
		return errors.Errorf("preparing object_store_placement clear statement: %w", err)
	}
	stmtInsertPlacement, err := sqlair.Prepare(`INSERT INTO "object_store_placement" (uuid, node_id) VALUES ($ObjectStorePlacement.uuid, $ObjectStorePlacement.node_id)`, v4_1_0.ObjectStorePlacement{})
	if err != nil {
		return errors.Errorf("preparing object_store_placement insert statement: %w", err)
	}
	stmtClearSequence, err := sqlair.Prepare(`DELETE FROM "sqlite_sequence"`)
	if err != nil {
		return errors.Errorf("preparing sqlite_sequence clear statement: %w", err)
	}

	if err := db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		// Merge the target password hash onto the restored model_agent row
		// when a target hash was captured (controller-model only).
		if o.ModelAgentPasswordHash != "" {
			for _, row := range p.ModelAgent {
				row.PasswordHash = &o.ModelAgentPasswordHash
				if err := tx.Query(ctx, stmtUpdate, row).Run(); err != nil {
					return errors.Errorf("merging model_agent password: %w", err)
				}
			}
		}

		// Merge the target machine-0 password material onto the restored
		// machine row, so the local machine-0 agent keeps working.
		if o.Machine0PasswordHash != nil {
			for _, row := range p.Machine {
				if row.Name == "0" {
					row.PasswordHash = o.Machine0PasswordHash
					row.PasswordHashAlgorithmID = o.Machine0AlgorithmID
					if err := tx.Query(ctx, stmtUpdateMachine, row).Run(); err != nil {
						return errors.Errorf("merging machine-0 password: %w", err)
					}
					break
				}
			}
		}

		// Graft the restored logical controller machine and unit onto the
		// recovery controller's substrate identity and network graph.
		if o.Machine0.NetNodeUUID != "" {
			for _, row := range o.NetNodes {
				if err := tx.Query(ctx, stmtInsertNetNode, row).Run(); err != nil {
					return errors.Errorf("inserting target net node: %w", err)
				}
			}
			for _, row := range o.LinkLayerDevices {
				if err := tx.Query(ctx, stmtInsertDevice, row).Run(); err != nil {
					return errors.Errorf("inserting target link layer device: %w", err)
				}
			}
			for _, row := range o.IPAddresses {
				// Target subnet UUIDs belong to the temporary model. The exact
				// address/device data is sufficient for API advertisement.
				row.SubnetUUID = nil
				if err := tx.Query(ctx, stmtInsertAddress, row).Run(); err != nil {
					return errors.Errorf("inserting target IP address: %w", err)
				}
			}
			if err := tx.Query(ctx, stmtUpdateMachineIdentity, o.Machine0).Run(); err != nil {
				return errors.Errorf("merging machine-0 identity: %w", err)
			}
			if o.ControllerUnitNetNodeUUID != "" {
				unit := v4_1_0.Unit{NetNodeUUID: o.ControllerUnitNetNodeUUID}
				if err := tx.Query(ctx, stmtUpdateControllerUnit, unit).Run(); err != nil {
					return errors.Errorf("merging controller unit network: %w", err)
				}
			}
			if o.Machine0CloudInstance != nil {
				instance := *o.Machine0CloudInstance
				for _, sourceMachine := range p.Machine {
					if sourceMachine.Name == "0" {
						instance.MachineUUID = sourceMachine.UUID
						break
					}
				}
				if err := tx.Query(ctx, stmtUpdateMachineInstance, instance).Run(); err != nil {
					return errors.Errorf("merging machine-0 instance: %w", err)
				}
			}
		}

		// Rewrite object-store placement to the single restore node.
		if err := tx.Query(ctx, stmtClearPlacement).Run(); err != nil {
			return errors.Errorf("clearing object_store_placement: %w", err)
		}
		for _, pl := range p.ObjectStorePlacement {
			pl.NodeID = o.NodeID
			if err := tx.Query(ctx, stmtInsertPlacement, pl).Run(); err != nil {
				return errors.Errorf("inserting object_store_placement: %w", err)
			}
		}

		// Clear stale changelog.
		if err := tx.Query(ctx, stmtClearChangeLog).Run(); err != nil {
			return errors.Errorf("clearing change_log: %w", err)
		}
		if err := tx.Query(ctx, stmtClearChangeLogWitness).Run(); err != nil {
			return errors.Errorf("clearing change_log_witness: %w", err)
		}

		// Remove restored AUTOINCREMENT entries so SQLite allocates after
		// current maxima.
		if err := tx.Query(ctx, stmtClearSequence).Run(); err != nil {
			return errors.Errorf("clearing sqlite_sequence: %w", err)
		}

		return nil
	}); err != nil {
		return errors.Errorf("applying model overlay: %w", err)
	}
	return nil
}
