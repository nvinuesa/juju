// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package domain

import (
	"sync"

	"github.com/juju/errors"

	"github.com/juju/juju/core/database"
)

// DBFactory defines a function that returns a database or an error if a
// database cannot be returned.
type DBFactory = func() (database.TxnRunner, error)

// StateBase defines a base struct for requesting a database. This will cache
// the database for the lifetime of the struct.
type StateBase struct {
	mu    sync.Mutex
	getDB DBFactory
	db    database.TxnRunner
}

// NewStateBase returns a new StateBase.
func NewStateBase(getDB DBFactory) *StateBase {
	return &StateBase{
		getDB: getDB,
	}
}

// DB returns the database for a given namespace.
func (st *StateBase) DB() (database.TxnRunner, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.getDB == nil {
		return nil, errors.New("nil getDB")
	}

	if st.db == nil {
		var err error
		if st.db, err = st.getDB(); err != nil {
			return nil, errors.Annotate(err, "invoking getDB")
		}
	}

	return st.db, nil
}