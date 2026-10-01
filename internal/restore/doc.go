// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package restore implements offline validation of controller backup
// archives for `juju bootstrap --restore` and the archive reading shared
// by the agent-side restore stage.
//
// A backup archive is a gzip-compressed tar with a single top-level
// juju-backup directory holding metadata.json, the controller database
// dump, one dump per model and the object blob bundle (root.tar).
// metadata.json records the exact agent version of the source controller;
// restore refuses any archive whose version does not equal the running
// binary's version.
package restore
