// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

// tablePolicy classifies how one dumped table is loaded into the freshly
// bootstrapped database.
type tablePolicy uint8

const (
	// policyReplace is the default: delete every target row and insert
	// the archived rows. The fresh database holds content that must be
	// replaced by the archived identity (lookup rows, spaces, object
	// store metadata — all of which are identical under the exact-version
	// gate, so replace is semantically idempotent).
	policyReplace tablePolicy = iota

	// policySkip keeps the target's bootstrapped content: controller
	// identity and node-local state that is regenerated or observed by
	// the running replacement and must never be imported stale.
	policySkip
)

// controllerTablePolicies lists tables that policySkip. Every table not
// listed here is policyReplace.
var controllerTablePolicies = map[string]tablePolicy{
	// Change-log machinery: log entries, namespace bookkeeping and
	// witness watermarks are owned by the (empty) fresh database; the
	// archived rows describe the source's runtime and must not be
	// imported. The namespace table is seeded by DDL and expanded by
	// trigger patches whose hardcoded IDs must match the fresh compiled
	// SQL.
	"change_log":           policySkip,
	"change_log_namespace": policySkip,
	"change_log_witness":   policySkip,

	// Controller-node identity: the replacement bootstraps its own node
	// row, agent version, nonce and password — the archived rows
	// describe the source's node that no longer exists.
	"controller_node":               policySkip,
	"controller_node_agent_version": policySkip,
	"controller_node_nonce":         policySkip,
	"controller_node_password":      policySkip,

	// controller_api_address is written from the replacement's
	// addresses by the API address setter worker after bootstrap.
	"controller_api_address": policySkip,

	// autocert_cache holds the replacement's own TLS auto-cert state;
	// the archived cache references the source controller's certificate.
	"autocert_cache": policySkip,

	// Upgrade machinery: the source may have had an in-flight or
	// completed upgrade whose state must not be resurrected.
	"upgrade_info":                 policySkip,
	"upgrade_info_controller_node": policySkip,

	// object_store_backend is the bootstrap-seeded file-backend row:
	// the fresh database's deterministic backend uuid must stay.
	// Recovery is always file-backed; the archive may contain an s3
	// backend row and its singleton partial indexes forbid two file
	// rows.
	"object_store_backend":           policySkip,
	"object_store_backend_s3_config": policySkip,

	// Drain history is not carried over to the replacement.
	"object_store_drain_info": policySkip,

	// The bootstrap flag gates the bootstrap worker: the archived flag
	// would uninstall the worker before it finalizes the replacement
	// (controller node password, API host ports, bootstrap unlock).
	"flag": policySkip,

	// schema records the DDL migrations the fresh database has applied;
	// the archived rows are identical under the exact-version gate, but
	// the target's schema table is authoritative for its DDL runtime.
	"schema": policySkip,
}

// modelTablePolicies lists tables that policySkip. Every table not listed
// here is policyReplace.
var modelTablePolicies = map[string]tablePolicy{
	// Change-log machinery: see controller comment.
	"change_log":           policySkip,
	"change_log_namespace": policySkip,
	"change_log_witness":   policySkip,

	// K8s pod and service state is observed by the replacement's k8s worker
	// from the running cluster; agent presence is regenerated from freshly
	// connected agents. Imported rows describe the source controller's pods
	// and heartbeats and must not shadow or fight the replacement's own
	// observations.
	"k8s_pod":                policySkip,
	"k8s_pod_port":           policySkip,
	"k8s_pod_status":         policySkip,
	"k8s_service":            policySkip,
	"machine_agent_presence": policySkip,
	"unit_agent_presence":    policySkip,

	// schema records the DDL migrations applied to this model database;
	// the target's rows are authoritative.
	"schema": policySkip,
}

// specialTables are handled outside the generic replay and must never be
// reached by it.
var specialTables = map[string]bool{
	// The seed row the replacement bootstrapped with is deleted and the
	// source row recovered (nothing references controller.uuid).
	"controller": true,
	// Placements are rebuilt for the single replacement node after the
	// metadata merge.
	"object_store_placement": true,
}
