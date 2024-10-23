// Copyright 2024 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

// controller is used to fetch the controller from the database.
type controller struct {
	UUID      string `db:"uuid"`
	ModelUUID string `db:"model_uuid"`
}
