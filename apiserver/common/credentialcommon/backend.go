// Copyright 2018 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package credentialcommon

import (
	"github.com/juju/errors"
	"github.com/juju/names/v5"

	credentialservice "github.com/juju/juju/domain/credential/service"
	"github.com/juju/juju/state"
)

type stateShim struct {
	*state.State
}

// AllMachines implements MachineService.AllMachines.
func (st stateShim) AllMachines() ([]credentialservice.Machine, error) {
	machines, err := st.State.AllMachines()
	if err != nil {
		return nil, errors.Trace(err)
	}
	result := make([]credentialservice.Machine, len(machines))
	for i, m := range machines {
		result[i] = m
	}
	return result, nil
}

// CloudCredentialTag returns the tag of the cloud credential used for managing the
// model's cloud resources, and a boolean indicating whether a credential is set.
func (st stateShim) CloudCredentialTag() (names.CloudCredentialTag, bool, error) {
	m, err := st.State.Model()
	if err != nil {
		return names.CloudCredentialTag{}, false, errors.Trace(err)
	}
	credTag, exists := m.CloudCredentialTag()
	return credTag, exists, nil
}
