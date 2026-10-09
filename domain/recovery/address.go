// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

import (
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/uuid"
)

// ControllerUnitAddress is a replacement observation persisted during recovery.
type ControllerUnitAddress struct {
	UUID  string
	Value string
	Type  network.AddressType
}

// ControllerUnitAddressPatch supplies identifiers before persistence starts.
// A device is created only when IP addresses need one and none is recorded.
type ControllerUnitAddressPatch struct {
	DeviceUUID string
	Addresses  []ControllerUnitAddress
}

// NewControllerUnitAddressPatch prepares IP and DNS observations for recovery.
func NewControllerUnitAddressPatch(values []string) (ControllerUnitAddressPatch, error) {
	var patch ControllerUnitAddressPatch
	if len(values) == 0 {
		return patch, nil
	}
	deviceUUID, err := uuid.NewUUID()
	if err != nil {
		return patch, errors.Capture(err)
	}
	patch.DeviceUUID = deviceUUID.String()
	seen := make(map[string]bool)
	for _, value := range values {
		if value == "" {
			return ControllerUnitAddressPatch{}, errors.New("empty controller unit address")
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		addressUUID, err := uuid.NewUUID()
		if err != nil {
			return ControllerUnitAddressPatch{}, errors.Capture(err)
		}
		patch.Addresses = append(patch.Addresses, ControllerUnitAddress{
			UUID: addressUUID.String(), Value: value,
			Type: network.DeriveAddressType(value),
		})
	}
	return patch, nil
}
