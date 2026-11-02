// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery

// CredentialPatch carries the replacement authentication for an archived
// credential: its identity is retained while the auth type and attributes
// are rebound.
type CredentialPatch struct {
	AuthType   string
	Attributes map[string]string
}
