// Copyright 2020 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelmanager

import (
	"github.com/juju/errors"
	"github.com/juju/names/v5"
)

type credentialStateShim struct {
	StateBackend
}

func (s credentialStateShim) CloudCredentialTag() (names.CloudCredentialTag, bool, error) {
	m, err := s.StateBackend.Model()
	if err != nil {
		return names.CloudCredentialTag{}, false, errors.Trace(err)
	}
	credTag, exists := m.CloudCredentialTag()
	return credTag, exists, nil
}
