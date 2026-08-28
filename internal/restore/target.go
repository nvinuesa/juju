// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"context"

	"github.com/canonical/sqlair"

	"github.com/juju/juju/core/database"
	"github.com/juju/juju/domain/export/types/v4_1_0"
	"github.com/juju/juju/internal/errors"
)

// Target holds the target-local facts captured before mutation. Capture is
// explicit: every value the importer or the compatibility checks use comes
// from this struct, never from hidden reads inside importer code.
type Target struct {
	// ControllerUUID is the temporary target controller UUID.
	ControllerUUID string

	// ControllerModelUUID is the temporary target controller-model UUID.
	ControllerModelUUID string

	// ControllerID is the target controller node ID ("0").
	ControllerID string

	// ModelNamespaces are the target's registered model namespaces.
	ModelNamespaces []string

	// ModelCount is the number of model rows in the target controller DB.
	// A fresh/disposable target has exactly one (its temporary controller
	// model).
	ModelCount int

	// CloudName/CloudType identify the target's single cloud.
	CloudName string
	CloudType string

	// Regions are the target cloud's region names.
	Regions []string

	// CredentialName/CredentialAuthType identify the target controller
	// model's cloud credential.
	CredentialName     string
	CredentialAuthType string

	// Applications is the number of application rows in the temporary
	// controller-model DB. A fresh/disposable target has none.
	Applications int

	// ModelAgentPasswordHash is the target controller-model agent password
	// hash, captured from the controller model DB.
	ModelAgentPasswordHash string

	// Machine0PasswordHash/AlgorithmID are the target machine-0 password
	// material, captured from the temporary controller model DB so the
	// target's local machine agent keeps working against the restored
	// controller model.
	Machine0PasswordHash *string
	Machine0AlgorithmID  *string

	// Machine0 and its network rows are target-local substrate identity. The
	// restored logical machine and controller unit are grafted onto these
	// facts so the recovery controller advertises and manages its own host.
	Machine0                  v4_1_0.Machine
	Machine0CloudInstance     *v4_1_0.MachineCloudInstance
	ControllerUnitNetNodeUUID string
	NetNodes                  []v4_1_0.NetNode
	LinkLayerDevices          []v4_1_0.LinkLayerDevice
	IPAddresses               []v4_1_0.IpAddress
}

type controllerRow struct {
	UUID      string `db:"uuid"`
	ModelUUID string `db:"model_uuid"`
}

type namespaceRow struct {
	Namespace string `db:"namespace"`
}

type modelCountRow struct {
	Count int `db:"count"`
}

type controllerNodeRow struct {
	ControllerID string `db:"controller_id"`
}

type cloudRow struct {
	Name string `db:"name"`
	Type string `db:"type"`
}

type regionRow struct {
	Name string `db:"name"`
}

type credentialRow struct {
	Name     string `db:"name"`
	AuthType string `db:"type"`
}

type applicationCountRow struct {
	Count int `db:"count"`
}

type modelAgentRow struct {
	PasswordHash string `db:"password_hash"`
}

type machineRow struct {
	PasswordHash            *string `db:"password_hash"`
	PasswordHashAlgorithmID *string `db:"password_hash_algorithm_id"`
}

// CaptureTarget reads the target-local controller facts from the controller
// database. It runs before any mutation and fails (pre-mutation) on any
// missing mapping. The controller-model rows (agent password hash and
// application count) are read from the controller model DB opened through
// getter.
func CaptureTarget(ctx context.Context, db database.TxnRunner, getter DBGetter) (*Target, error) {
	t := &Target{}
	var err error

	stmtController, err := sqlair.Prepare(`SELECT &controllerRow.* FROM "controller"`, controllerRow{})
	if err != nil {
		return nil, errors.Errorf("preparing controller capture statement: %w", err)
	}
	stmtModelCount, err := sqlair.Prepare(`SELECT COUNT(*) AS &modelCountRow.count FROM "model"`, modelCountRow{})
	if err != nil {
		return nil, errors.Errorf("preparing model count capture statement: %w", err)
	}
	stmtNode, err := sqlair.Prepare(`SELECT &controllerNodeRow.* FROM "controller_node"`, controllerNodeRow{})
	if err != nil {
		return nil, errors.Errorf("preparing controller_node capture statement: %w", err)
	}
	stmtCloud, err := sqlair.Prepare(`SELECT &cloudRow.* FROM "cloud" JOIN "cloud_type" ON cloud_type_id = id`, cloudRow{})
	if err != nil {
		return nil, errors.Errorf("preparing cloud capture statement: %w", err)
	}
	stmtRegions, err := sqlair.Prepare(`SELECT &regionRow.* FROM "cloud_region"`, regionRow{})
	if err != nil {
		return nil, errors.Errorf("preparing region capture statement: %w", err)
	}
	stmtCredential, err := sqlair.Prepare(
		`SELECT name AS &credentialRow.name, type AS &credentialRow.type FROM "cloud_credential" JOIN "auth_type" ON auth_type_id = id WHERE uuid IN (SELECT cloud_credential_uuid FROM "model")`,
		credentialRow{})
	if err != nil {
		return nil, errors.Errorf("preparing credential capture statement: %w", err)
	}
	stmtNamespace, err := sqlair.Prepare(`SELECT &namespaceRow.* FROM "namespace_list"`, namespaceRow{})
	if err != nil {
		return nil, errors.Errorf("preparing namespace_list capture statement: %w", err)
	}

	if err := db.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		var controller controllerRow
		if err := tx.Query(ctx, stmtController).Get(&controller); err != nil {
			return errors.Errorf("reading target controller row: %w", err)
		}
		t.ControllerUUID = controller.UUID
		t.ControllerModelUUID = controller.ModelUUID

		var mc modelCountRow
		if err := tx.Query(ctx, stmtModelCount).Get(&mc); err != nil {
			return errors.Errorf("counting target models: %w", err)
		}
		t.ModelCount = mc.Count

		var node controllerNodeRow
		if err := tx.Query(ctx, stmtNode).Get(&node); err != nil {
			return errors.Errorf("reading target controller_node: %w", err)
		}
		t.ControllerID = node.ControllerID

		var cloud cloudRow
		if err := tx.Query(ctx, stmtCloud).Get(&cloud); err != nil {
			return errors.Errorf("reading target cloud: %w", err)
		}
		t.CloudName = cloud.Name
		t.CloudType = cloud.Type

		var regionRows []regionRow
		if err := tx.Query(ctx, stmtRegions).GetAll(&regionRows); err != nil && !errors.Is(err, sqlair.ErrNoRows) {
			return errors.Errorf("reading target regions: %w", err)
		}
		for _, r := range regionRows {
			t.Regions = append(t.Regions, r.Name)
		}

		var cred credentialRow
		if err := tx.Query(ctx, stmtCredential).Get(&cred); err != nil && !errors.Is(err, sqlair.ErrNoRows) {
			return errors.Errorf("reading target credential: %w", err)
		}
		t.CredentialName = cred.Name
		t.CredentialAuthType = cred.AuthType

		var nsRows []namespaceRow
		if err := tx.Query(ctx, stmtNamespace).GetAll(&nsRows); err != nil && !errors.Is(err, sqlair.ErrNoRows) {
			return errors.Errorf("reading target namespace_list: %w", err)
		}
		for _, r := range nsRows {
			t.ModelNamespaces = append(t.ModelNamespaces, r.Namespace)
		}
		return nil
	}); err != nil {
		return nil, errors.Errorf("capturing target overlay: %w", err)
	}

	// Capture the controller-model rows from the controller model DB.
	modelDB, err := getter.GetDB(ctx, t.ControllerModelUUID)
	if err != nil {
		return nil, errors.Errorf("opening controller model DB %q: %w", t.ControllerModelUUID, err)
	}
	if err := t.captureControllerModel(ctx, modelDB); err != nil {
		return nil, err
	}
	return t, nil
}

// captureControllerModel reads the controller-model agent password hash and
// the application count from the temporary controller model DB.
func (t *Target) captureControllerModel(ctx context.Context, modelDB database.TxnRunner) error {
	stmtAgent, err := sqlair.Prepare(`SELECT &modelAgentRow.* FROM "model_agent"`, modelAgentRow{})
	if err != nil {
		return errors.Errorf("preparing model_agent capture statement: %w", err)
	}
	stmtMachine, err := sqlair.Prepare(`SELECT &machineRow.* FROM "machine" WHERE name = '0'`, machineRow{})
	if err != nil {
		return errors.Errorf("preparing machine capture statement: %w", err)
	}
	stmtApps, err := sqlair.Prepare(`SELECT COUNT(*) AS &applicationCountRow.count FROM "application"`, applicationCountRow{})
	if err != nil {
		return errors.Errorf("preparing application count capture statement: %w", err)
	}

	stmtMachineIdentity, err := sqlair.Prepare(`SELECT &Machine.* FROM "machine" WHERE name = '0'`, v4_1_0.Machine{})
	if err != nil {
		return errors.Errorf("preparing machine identity capture statement: %w", err)
	}
	stmtMachineInstance, err := sqlair.Prepare(`SELECT &MachineCloudInstance.* FROM "machine_cloud_instance" WHERE machine_uuid = $Machine.uuid`, v4_1_0.MachineCloudInstance{}, v4_1_0.Machine{})
	if err != nil {
		return errors.Errorf("preparing machine instance capture statement: %w", err)
	}
	stmtControllerUnit, err := sqlair.Prepare(`SELECT &Unit.* FROM "unit" WHERE name = 'controller/0'`, v4_1_0.Unit{})
	if err != nil {
		return errors.Errorf("preparing controller unit capture statement: %w", err)
	}
	stmtNetNodes, err := sqlair.Prepare(`SELECT &NetNode.* FROM "net_node"`, v4_1_0.NetNode{})
	if err != nil {
		return errors.Errorf("preparing net node capture statement: %w", err)
	}
	stmtDevices, err := sqlair.Prepare(`SELECT &LinkLayerDevice.* FROM "link_layer_device"`, v4_1_0.LinkLayerDevice{})
	if err != nil {
		return errors.Errorf("preparing link layer device capture statement: %w", err)
	}
	stmtAddresses, err := sqlair.Prepare(`SELECT &IpAddress.* FROM "ip_address"`, v4_1_0.IpAddress{})
	if err != nil {
		return errors.Errorf("preparing IP address capture statement: %w", err)
	}

	err = modelDB.Txn(ctx, func(ctx context.Context, tx *sqlair.TX) error {
		var agent modelAgentRow
		if err := tx.Query(ctx, stmtAgent).Get(&agent); err != nil {
			return errors.Errorf("reading target model_agent: %w", err)
		}
		t.ModelAgentPasswordHash = agent.PasswordHash

		var machine machineRow
		if err := tx.Query(ctx, stmtMachine).Get(&machine); err != nil {
			return errors.Errorf("reading target machine-0 password: %w", err)
		}
		t.Machine0PasswordHash = machine.PasswordHash
		t.Machine0AlgorithmID = machine.PasswordHashAlgorithmID

		if err := tx.Query(ctx, stmtMachineIdentity).Get(&t.Machine0); err != nil {
			return errors.Errorf("reading target machine-0 identity: %w", err)
		}
		var instance v4_1_0.MachineCloudInstance
		if err := tx.Query(ctx, stmtMachineInstance, t.Machine0).Get(&instance); err != nil {
			if !errors.Is(err, sqlair.ErrNoRows) {
				return errors.Errorf("reading target machine-0 instance: %w", err)
			}
		} else {
			t.Machine0CloudInstance = &instance
		}

		var controllerUnit v4_1_0.Unit
		if err := tx.Query(ctx, stmtControllerUnit).Get(&controllerUnit); err != nil {
			if !errors.Is(err, sqlair.ErrNoRows) {
				return errors.Errorf("reading target controller unit: %w", err)
			}
		} else {
			t.ControllerUnitNetNodeUUID = controllerUnit.NetNodeUUID
		}

		var netNodes []v4_1_0.NetNode
		if err := tx.Query(ctx, stmtNetNodes).GetAll(&netNodes); err != nil && !errors.Is(err, sqlair.ErrNoRows) {
			return errors.Errorf("reading target net nodes: %w", err)
		}
		t.NetNodes = netNodes

		var devices []v4_1_0.LinkLayerDevice
		if err := tx.Query(ctx, stmtDevices).GetAll(&devices); err != nil && !errors.Is(err, sqlair.ErrNoRows) {
			return errors.Errorf("reading target link layer devices: %w", err)
		}
		t.LinkLayerDevices = devices

		var addresses []v4_1_0.IpAddress
		if err := tx.Query(ctx, stmtAddresses).GetAll(&addresses); err != nil && !errors.Is(err, sqlair.ErrNoRows) {
			return errors.Errorf("reading target IP addresses: %w", err)
		}
		t.IPAddresses = addresses

		var apps applicationCountRow
		if err := tx.Query(ctx, stmtApps).Get(&apps); err != nil {
			return errors.Errorf("counting target applications: %w", err)
		}
		t.Applications = apps.Count
		return nil
	})
	if err != nil {
		return errors.Capture(err)
	}
	t.keepControllerNetwork()
	return nil
}

// keepControllerNetwork drops unrelated target network rows from the overlay.
func (t *Target) keepControllerNetwork() {
	nodes := map[string]bool{t.Machine0.NetNodeUUID: true}
	if t.ControllerUnitNetNodeUUID != "" {
		nodes[t.ControllerUnitNetNodeUUID] = true
	}

	t.NetNodes = filter(t.NetNodes, func(row v4_1_0.NetNode) bool {
		return nodes[row.UUID]
	})
	t.LinkLayerDevices = filter(t.LinkLayerDevices, func(row v4_1_0.LinkLayerDevice) bool {
		return nodes[row.NetNodeUUID]
	})
	t.IPAddresses = filter(t.IPAddresses, func(row v4_1_0.IpAddress) bool {
		return nodes[row.NetNodeUUID]
	})
}

func filter[T any](rows []T, keep func(T) bool) []T {
	result := make([]T, 0, len(rows))
	for _, row := range rows {
		if keep(row) {
			result = append(result, row)
		}
	}
	return result
}
