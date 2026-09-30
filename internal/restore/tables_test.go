// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	stdtesting "testing"

	"github.com/juju/tc"
)

type tablesSuite struct{}

func TestTablesSuite(t *stdtesting.T) {
	tc.Run(t, &tablesSuite{})
}

// defaultInsertControllerTables records, at audit time, every controller
// schema table with no entry in controllerTablePolicies or specialTables:
// the explicit decision that these load verbatim (policyInsert). A new
// schema table absent from this list and from the policy maps fails
// TestTablePoliciesCoverSchema, forcing a classification before it can
// reach a dump — unknown data must never be imported silently.
var defaultInsertControllerTables = []string{
	"agent_binary_store", "bakery_config", "charm_tracing_config", "cloud", "cloud_auth_type", "cloud_ca_cert", "cloud_credential",
	"cloud_credential_attribute", "cloud_defaults", "cloud_image_metadata", "cloud_region", "cloud_region_defaults", "controller_config", "controller_ssh_host_key",
	"external_controller", "external_controller_address", "external_model", "lease", "lease_pin", "logging_loki_config", "macaroon_root_key",
	"model", "model_authorized_keys", "model_database_deletion", "model_last_login", "model_migration_export", "model_migration_export_minion_sync", "model_migration_export_offer",
	"model_migration_export_phase", "model_migration_export_status", "model_migration_export_target_auth", "model_migration_import", "model_migration_import_external_controller_model", "model_migration_import_offer", "model_migration_redirect",
	"model_migration_redirect_user", "model_namespace", "model_secret_backend", "namespace_list", "permission", "secret_backend", "secret_backend_config",
	"secret_backend_reference", "secret_backend_rotation", "user", "user_activation_key", "user_authentication", "user_password", "user_public_ssh_key",
	"workload_tracing_config",
}

// defaultInsertModelTables is the model database counterpart of
// defaultInsertControllerTables.
var defaultInsertModelTables = []string{
	"agent_binary_store", "agent_version", "annotation_application", "annotation_charm", "annotation_machine", "annotation_model", "annotation_storage_filesystem",
	"annotation_storage_instance", "annotation_storage_volume", "annotation_unit", "application", "application_agent", "application_channel", "application_config",
	"application_config_hash", "application_constraint", "application_controller", "application_endpoint", "application_exposed_endpoint_cidr", "application_exposed_endpoint_space", "application_extra_endpoint",
	"application_k8s_resources_managed", "application_platform", "application_platform_new", "application_remote_consumer", "application_remote_offerer", "application_remote_offerer_relation_macaroon", "application_remote_offerer_status",
	"application_resource", "application_scale", "application_setting", "application_status", "application_storage_directive", "application_workload_version", "availability_zone",
	"availability_zone_subnet", "block_command", "block_device", "block_device_link_device", "block_device_link_device_new", "block_device_new", "charm",
	"charm_action", "charm_category", "charm_config", "charm_container", "charm_container_mount", "charm_device", "charm_download_info",
	"charm_extra_binding", "charm_hash", "charm_hash_new", "charm_manifest_base", "charm_metadata", "charm_relation", "charm_resource",
	"charm_storage", "charm_storage_property", "charm_tag", "charm_term", "constraint", "constraint_space", "constraint_tag",
	"constraint_zone", "device_constraint", "device_constraint_attribute", "fqdn_address", "hostname_address", "instance_tag", "ip_address",
	"link_layer_device", "link_layer_device_dns_address", "link_layer_device_dns_domain", "link_layer_device_parent", "link_layer_device_route", "machine", "machine_agent_version",
	"machine_cloud_instance", "machine_cloud_instance_status", "machine_constraint", "machine_container_type", "machine_filesystem", "machine_lxd_profile", "machine_manual",
	"machine_parent", "machine_placement", "machine_platform", "machine_reprovision", "machine_requires_reboot", "machine_ssh_host_key", "machine_status",
	"machine_virtual_ssh_host_key", "machine_volume", "model", "model_agent", "model_config", "model_constraint", "model_life",
	"model_migrating", "model_storage_pool", "net_node", "net_node_fqdn_address", "net_node_hostname_address", "offer", "offer_connection",
	"offer_endpoint", "operation", "operation_action", "operation_machine_task", "operation_parameter", "operation_task", "operation_task_log",
	"operation_task_output", "operation_task_status", "operation_unit_task", "operator_status", "pending_application_resource", "port_range", "provider_ip_address",
	"provider_link_layer_device", "provider_network", "provider_network_subnet", "provider_space", "provider_subnet", "relation", "relation_application_setting",
	"relation_application_settings_hash", "relation_endpoint", "relation_network_egress", "relation_network_ingress", "relation_status", "relation_unit", "relation_unit_setting",
	"relation_unit_setting_archive", "relation_unit_settings_hash", "removal", "resource", "resource_container_image_metadata_store", "resource_file_store", "resource_image_store",
	"resource_retrieved_by", "secret", "secret_application_owner", "secret_content", "secret_content_new", "secret_deleted_value_ref", "secret_metadata",
	"secret_model_owner", "secret_permission", "secret_reference", "secret_reference_new", "secret_remote_unit_consumer", "secret_reservation", "secret_revision",
	"secret_revision_expire", "secret_revision_obsolete", "secret_rotation", "secret_unit_consumer", "secret_unit_owner", "secret_value_ref", "sequence",
	"ssh_connection_request", "storage_attachment", "storage_filesystem", "storage_filesystem_attachment", "storage_filesystem_status", "storage_instance", "storage_instance_filesystem",
	"storage_instance_volume", "storage_pool", "storage_pool_attribute", "storage_unit_owner", "storage_volume", "storage_volume_attachment", "storage_volume_attachment_new",
	"storage_volume_attachment_plan", "storage_volume_attachment_plan_attr", "storage_volume_status", "subnet", "unit", "unit_agent_status", "unit_agent_version",
	"unit_principal", "unit_resolved", "unit_resource", "unit_state", "unit_state_charm", "unit_state_relation", "unit_storage_directive",
	"unit_virtual_ssh_host_key", "unit_workload_status", "unit_workload_version",
}

var createTableRE = regexp.MustCompile(`(?i)CREATE TABLE (?:IF NOT EXISTS )?"?([a-zA-Z_0-9]+)"?`)

// schemaTables reads the schema DDL shipped in the repository and returns
// every table name it creates. Tests run with the package directory as the
// working directory, so the domain schema is two levels up.
func schemaTables(c *tc.C, patterns ...string) []string {
	var tables []string
	seen := make(map[string]struct{})
	for _, pattern := range patterns {
		files, err := filepath.Glob(filepath.Join("..", "..", pattern))
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(files, tc.Not(tc.HasLen), 0, tc.Commentf("no files for %s", pattern))
		for _, file := range files {
			data, err := os.ReadFile(file)
			c.Assert(err, tc.ErrorIsNil)
			for _, m := range createTableRE.FindAllStringSubmatch(string(data), -1) {
				name := m[1]
				if _, dup := seen[name]; dup {
					continue
				}
				seen[name] = struct{}{}
				tables = append(tables, name)
			}
		}
	}
	return tables
}

func (s *tablesSuite) TestTablePoliciesCoverSchema(c *tc.C) {
	for _, tt := range []struct {
		name          string
		patterns      []string
		policies      map[string]tablePolicy
		specialTables []string
		defaultInsert []string
	}{
		{
			name:          "controller",
			patterns:      []string{"domain/schema/controller/sql/*.sql"},
			policies:      controllerTablePolicies,
			specialTables: []string{"controller", "object_store_placement"},
			defaultInsert: defaultInsertControllerTables,
		},
		{
			name:          "model",
			patterns:      []string{"domain/schema/model/sql/*.sql", "domain/schema/model/*.ddl"},
			policies:      modelTablePolicies,
			specialTables: []string{"object_store_placement"},
			defaultInsert: defaultInsertModelTables,
		},
	} {
		tables := schemaTables(c, tt.patterns...)

		// The migration framework creates the schema bookkeeping table
		// itself; it is not in the DDL files.
		tables = append(tables, "schema")

		// Every schema table must have an explicit policy decision:
		// classified in the policy maps, handled outside the generic
		// replay, or recorded as an accepted verbatim insert.
		var unclassified []string
		for _, table := range tables {
			if _, ok := tt.policies[table]; ok {
				continue
			}
			if slices.Contains(tt.specialTables, table) {
				continue
			}
			if slices.Contains(tt.defaultInsert, table) {
				continue
			}
			unclassified = append(unclassified, table)
		}
		c.Check(unclassified, tc.HasLen, 0, tc.Commentf(
			"schema tables without an explicit restore policy: %v", unclassified))

		// Every policy entry must name a table the schema actually
		// creates: a stale entry silently changes behaviour after a
		// schema rename.
		known := make(map[string]struct{}, len(tables))
		for _, table := range tables {
			known[table] = struct{}{}
		}
		var stale []string
		for table := range tt.policies {
			if _, ok := known[table]; !ok {
				stale = append(stale, table)
			}
		}
		for _, table := range tt.specialTables {
			if _, ok := known[table]; !ok {
				stale = append(stale, table)
			}
		}
		c.Check(stale, tc.HasLen, 0, tc.Commentf(
			"policy entries for tables the schema does not create: %v", stale))
	}
}

// TestUnknownTableIsPolicyInsert pins the loader's behaviour for a table
// that reaches a dump without classification: it loads verbatim, because
// the zero tablePolicy value is policyInsert. The tripwire test above
// keeps that from ever happening to a real schema table.
func (s *tablesSuite) TestUnknownTableIsPolicyInsert(c *tc.C) {
	c.Check(tablePolicy(0), tc.Equals, policyInsert)
	c.Check(controllerTablePolicies["no-such-table"], tc.Equals, policyInsert)
}
