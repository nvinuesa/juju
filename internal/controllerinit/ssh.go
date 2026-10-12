// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controllerinit

import (
	"github.com/juju/utils/v4/ssh"

	"github.com/juju/juju/internal/errors"
)

// DeleteSSHKeys removes temporary provisioning keys from the Ubuntu account.
func DeleteSSHKeys(keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	fingerprints := make([]string, 0, len(keys))
	for _, key := range keys {
		fingerprint, _, err := ssh.KeyFingerprint(key)
		if err != nil {
			return errors.Capture(err)
		}
		fingerprints = append(fingerprints, fingerprint)
	}
	return ssh.DeleteKeysFromFile("ubuntu", "authorized_keys", fingerprints)
}
