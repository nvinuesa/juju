// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package service

import (
	"context"
	"slices"

	"github.com/juju/juju/core/lease"
	coremodel "github.com/juju/juju/core/model"
	"github.com/juju/juju/core/semversion"
	domainexport "github.com/juju/juju/domain/export"
	domainrecovery "github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/internal/errors"
)

// LoadControllerDump validates native YAML and removes lost-controller leases
// before replacing controller data in a single transaction.
func (s *Service) LoadControllerDump(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return errors.Capture(err)
	}
	dump, err := decodeDump(data)
	if err != nil {
		return errors.Capture(err)
	}
	if err := checkDumpVersion(dump, []semversion.Number{domainexport.LatestControllerExportVersion()}); err != nil {
		return errors.Errorf("controller dump: %w", err)
	}
	if len(dump.Tables["controller"]) == 0 {
		return errors.New("controller dump has no controller row")
	}
	decoded, err := decodeControllerDump(dump)
	if err != nil {
		return errors.Capture(err)
	}
	discardBackupLeases(decoded)
	selectTables(decoded.Present, controllerTablePolicies)
	return errors.Capture(s.controller.LoadControllerDump(ctx, decoded))
}

// LoadModelDump validates native YAML before opening and replacing model data.
func (s *Service) LoadModelDump(ctx context.Context, model coremodel.UUID, data []byte) error {
	if err := ctx.Err(); err != nil {
		return errors.Capture(err)
	}
	if err := model.Validate(); err != nil {
		return errors.Capture(err)
	}
	dump, err := decodeDump(data)
	if err != nil {
		return errors.Capture(err)
	}
	if err := checkDumpVersion(dump, []semversion.Number{domainexport.LatestSupportedPayloadVersion()}); err != nil {
		return errors.Errorf("model dump: %w", err)
	}
	decoded, err := decodeModelDump(dump)
	if err != nil {
		return errors.Capture(err)
	}
	selectTables(decoded.Present, modelTablePolicies)
	st, err := s.modelState(ctx, model)
	if err != nil {
		return errors.Capture(err)
	}
	if err := st.LoadModelDump(ctx, decoded); err != nil {
		return errors.Errorf("model %q: %w", model, err)
	}
	return nil
}

func selectTables(present map[string]bool, policies map[string]tablePolicy) {
	for table := range present {
		if policies[table] == policySkip || specialTables[table] {
			delete(present, table)
		}
	}
}

// Backup leases describe an in-flight operation on the lost controller. They
// must not block backups on its replacement. Decoding created private typed
// rows, so filtering does not mutate the supplied archive or affect retries.
func discardBackupLeases(dump *domainrecovery.ControllerDump) {
	types := make(map[int64]bool)
	for _, row := range dump.ControllerLeaseType {
		if row.Type != nil && *row.Type == lease.BackupCreationNamespace && row.ID != nil {
			types[*row.ID] = true
		}
	}
	discarded := make(map[string]bool)
	for _, row := range dump.ControllerLease {
		if row.LeaseTypeID != nil && types[*row.LeaseTypeID] && row.UUID != nil {
			discarded[*row.UUID] = true
		}
	}
	dump.ControllerLease = slices.DeleteFunc(dump.ControllerLease, func(row domainrecovery.ControllerLease) bool {
		return row.UUID != nil && discarded[*row.UUID]
	})
	dump.ControllerLeasePin = slices.DeleteFunc(dump.ControllerLeasePin, func(row domainrecovery.ControllerLeasePin) bool {
		return row.LeaseUUID != nil && discarded[*row.LeaseUUID]
	})
}

// checkDumpVersion enforces the loader's native dump formats: a dump is
// only ever loaded when its envelope version names an export format this
// binary produces. The agent-version gate implies matching formats, but
// the envelope is the actual contract and is checked where the load runs.
func checkDumpVersion(dump *dump, supported []semversion.Number) error {
	if dump.Version == "" {
		return errors.Errorf("dump records no format version")
	}
	parsed, err := semversion.Parse(dump.Version)
	if err != nil {
		return errors.Errorf("dump records unparseable format version %q", dump.Version)
	}
	if !slices.Contains(supported, parsed) {
		return errors.Errorf("unsupported dump format version %q: this loader reads %v", dump.Version, supported)
	}
	return nil
}
