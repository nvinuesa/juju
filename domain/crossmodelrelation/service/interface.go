// Copyright 2025 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

// SecretRevisionChange holds information about a secret revision change returned from state.
type SecretRevisionChange struct {
	URI      string
	Revision int
}
