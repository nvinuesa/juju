// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package service validates native backup dumps and coordinates recovery
// persistence during exclusive bootstrap. Recovery policies live here;
// generated row bindings and transactional SQL live behind state interfaces.
package service
