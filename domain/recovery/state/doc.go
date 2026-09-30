// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package state loads archived database dumps into the freshly
// bootstrapped controller and model databases during recovery bootstrap.
//
// The load is a one-shot operation run before any worker starts: one
// transaction per database, foreign keys disabled while replaying,
// foreign_key_check proving integrity before commit. Every archived
// table replaces the target's rows except the few policy-skip tables
// that carry the replacement's identity or runtime state. After the
// replay the controller row is swapped to the source identity and object
// store placements are rebuilt for the single replacement node.
package state
