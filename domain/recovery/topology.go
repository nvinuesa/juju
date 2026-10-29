// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"slices"
	"strconv"
	"strings"

	"github.com/juju/juju/internal/errors"
)

// SingleControllerDump retires lost controller peers in a copy of the dump.
// It preserves unrelated entities and never requests provider deletion. The
// archived input remains reusable if import fails and a new target is created.
func SingleControllerDump(dump *ModelDump, controllerID string) (*ModelDump, error) {
	controllers := dump.ApplicationController
	if len(controllers) == 0 {
		return dump, nil
	}
	if len(controllers) != 1 {
		return nil, errors.New("archive must identify one controller application")
	}
	application := controllers[0].ApplicationUUID

	// nodes and units cover the controller application only: workload units
	// sharing a controller host are a rejection, not a rewrite.
	nodes := make(map[string]bool)
	units := make(map[string]bool)
	var replacementNode string
	var retainedUnit, retainedName string
	for _, machine := range dump.Machine {
		if machine.Name == controllerID && machine.LifeID == 0 {
			replacementNode = machine.NetNodeUUID
		}
	}
	for _, unit := range dump.Unit {
		if unit.ApplicationUUID != application {
			continue
		}
		nodes[unit.NetNodeUUID] = true
		units[unit.UUID] = true
		selected := replacementNode != "" && unit.NetNodeUUID == replacementNode
		if len(dump.Machine) == 0 {
			selected = unit.Name == "controller/"+controllerID
		}
		if selected && unit.LifeID == 0 {
			if retainedUnit != "" {
				return nil, errors.New("replacement maps to multiple controller units")
			}
			retainedUnit, retainedName = unit.UUID, unit.Name
		}
	}
	if retainedUnit == "" {
		return nil, errors.New("archive has no live controller unit for the replacement")
	}
	for _, unit := range dump.Unit {
		if !units[unit.UUID] && nodes[unit.NetNodeUUID] && unit.LifeID != 2 {
			return nil, errors.Errorf(
				"controller shares a machine with workload unit %q; recovery would replace or retire its host",
				unit.Name)
		}
	}
	machines := make(map[string]bool)
	for _, machine := range dump.Machine {
		if nodes[machine.NetNodeUUID] {
			machines[machine.UUID] = true
		}
	}
	for _, parent := range dump.MachineParent {
		if machines[parent.ParentUUID] {
			return nil, errors.New("controller machine hosts containers; recovery would replace or retire their host")
		}
	}

	filtered := *dump
	filtered.Unit = slices.Clone(dump.Unit)
	for i := range filtered.Unit {
		unit := &filtered.Unit[i]
		if units[unit.UUID] && unit.UUID != retainedUnit {
			unit.LifeID = 2
		}
	}
	filtered.Machine = slices.Clone(dump.Machine)
	for i := range filtered.Machine {
		machine := &filtered.Machine[i]
		if machines[machine.UUID] && machine.Name != controllerID {
			machine.LifeID = 2
			machine.KeepInstance = new(true)
		}
	}
	filtered.MachineCloudInstance = slices.Clone(dump.MachineCloudInstance)
	for i := range filtered.MachineCloudInstance {
		instance := &filtered.MachineCloudInstance[i]
		for _, machine := range dump.Machine {
			if machines[machine.UUID] && machine.Name != controllerID &&
				instance.MachineUUID == machine.UUID {
				instance.LifeID = 2
			}
		}
	}
	filtered.ApplicationScale = slices.Clone(dump.ApplicationScale)
	for i := range filtered.ApplicationScale {
		scale := &filtered.ApplicationScale[i]
		if scale.ApplicationUUID == application {
			ordinal, err := strconv.ParseInt(strings.TrimPrefix(retainedName, "controller/"), 10, 64)
			if err != nil {
				return nil, errors.Capture(err)
			}
			scale.Scale = new(int64(1))
			scale.ScaleTarget = new(int64(1))
			scale.Scaling = new(false)
			scale.StartOrdinal = ordinal
		}
	}
	return &filtered, nil
}

// new(expr) allocates a fresh copy of expr, so cloned dump rows never write
// through a pointer shared with the archived input.

func stringOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ValidateSingleController checks that the archive's controller topology
// supports a single-node replacement: one controller application, one live
// controller unit for the replacement, no workload units sharing controller
// hosts and no containers hosted on controller machines. It mirrors the
// checks SingleControllerDump makes before rewriting, for the preflight
// topology bag read offline.
func ValidateSingleController(topology *ControllerTopology, controllerID string) error {
	if topology == nil || len(topology.ApplicationController) != 1 {
		return errors.New("archive must identify one controller application")
	}
	application := topology.ApplicationController[0].ApplicationUUID

	nodes := make(map[string]bool)
	units := make(map[string]bool)
	var replacementNode string
	var retainedUnit string
	for _, machine := range topology.Machines {
		if machine.Name == controllerID && machine.LifeID == 0 {
			replacementNode = machine.NetNodeUUID
		}
	}
	for _, unit := range topology.Units {
		if unit.ApplicationUUID != application {
			continue
		}
		nodes[unit.NetNodeUUID] = true
		units[unit.UUID] = true
		selected := replacementNode != "" && unit.NetNodeUUID == replacementNode
		if len(topology.Machines) == 0 {
			selected = unit.Name == "controller/"+controllerID
		}
		if selected && unit.LifeID == 0 {
			if retainedUnit != "" {
				return errors.New("replacement maps to multiple controller units")
			}
			retainedUnit = unit.UUID
		}
	}
	if retainedUnit == "" {
		return errors.New("archive has no live controller unit for the replacement")
	}
	for _, unit := range topology.Units {
		if !units[unit.UUID] && nodes[unit.NetNodeUUID] && unit.LifeID != 2 {
			return errors.Errorf(
				"controller shares a machine with workload unit %q; recovery would replace or retire its host",
				unit.Name)
		}
	}
	machines := make(map[string]bool)
	for _, machine := range topology.Machines {
		if nodes[machine.NetNodeUUID] {
			machines[machine.UUID] = true
		}
	}
	for _, parent := range topology.MachineParents {
		if machines[parent.ParentUUID] {
			return errors.New("controller machine hosts containers; recovery would replace or retire their host")
		}
	}
	return nil
}
