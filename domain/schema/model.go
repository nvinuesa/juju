// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package schema

import (
	"github.com/juju/juju/core/database/schema"
)

const (
	tableModelConfig tableNamespaceID = iota + 1
)

// ModelDDL is used to create model databases.
func ModelDDL() *schema.Schema {
	patches := []func() schema.Patch{
		changeLogSchema,
		changeLogModelNamespace,
		modelConfig,
		changeLogTriggersForTable("model_config", "key", tableModelConfig),
		spaceSchema,
		subnetSchema,
	}

	schema := schema.New()
	for _, fn := range patches {
		schema.Add(fn())
	}
	return schema
}

func changeLogModelNamespace() schema.Patch {
	// Note: These should match exactly the values of the tableNamespaceID
	// constants above.
	return schema.MakePatch(`
INSERT INTO change_log_namespace VALUES
    (1, 'model_config', 'model config changes based on config key')
`)
}

func modelConfig() schema.Patch {
	return schema.MakePatch(`
CREATE TABLE model_config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`)
}

func spaceSchema() schema.Patch {
	return schema.MakePatch(`
CREATE TABLE provider_space (
    provider_id     TEXT PRIMARY KEY,
    space_uuid      TEXT NOT NULL,
    CONSTRAINT      fk_provider_space_space_uuid
        FOREIGN KEY     (space_uuid)
        REFERENCES      space(uuid)
);

CREATE UNIQUE INDEX idx_provider_space_space_uuid
ON provider_space (space_uuid);

CREATE TABLE space (
    uuid            TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    is_public       BOOLEAN
);

CREATE UNIQUE INDEX idx_spaces_uuid_name
ON space (name);
`)
}

func subnetSchema() schema.Patch {
	return schema.MakePatch(`
CREATE TABLE provider_subnet (
    provider_id     TEXT PRIMARY KEY,
    subnet_uuid     TEXT NOT NULL,
    CONSTRAINT      fk_provider_subnet_subnet_uuid
        FOREIGN KEY     (subnet_uuid)
        REFERENCES      subnet(uuid)
);

CREATE UNIQUE INDEX idx_provider_subnet_subnet_uuid
ON provider_subnet (subnet_uuid);

CREATE TABLE provider_network (
    uuid                TEXT PRIMARY KEY,
    provider_network_id TEXT
);

CREATE TABLE provider_network_subnet (
    uuid                  TEXT PRIMARY KEY,
    provider_network_uuid TEXT NOT NULL,
    subnet_uuid           TEXT NOT NULL,
    CONSTRAINT            fk_provider_network_subnet_provider_network_uuid
        FOREIGN KEY           (provider_network_uuid)
        REFERENCES            provider_network_uuid(uuid)
    CONSTRAINT            fk_provider_network_subnet_uuid
        FOREIGN KEY           (subnet_uuid)
        REFERENCES            subnet(uuid)
);

CREATE UNIQUE INDEX idx_provider_network_subnet_uuid
ON provider_network_subnet (subnet_uuid);

CREATE TABLE availability_zone (
    uuid            TEXT PRIMARY KEY,
    name            TEXT
);

CREATE TABLE availability_zone_subnet (
    uuid                   TEXT PRIMARY KEY,
    availability_zone_uuid TEXT NOT NULL,
    subnet_uuid            TEXT NOT NULL,
    CONSTRAINT             fk_availability_zone_availability_zone_uuid
        FOREIGN KEY            (availability_zone_uuid)
        REFERENCES             availability_zone(uuid)
    CONSTRAINT             fk_availability_zone_subnet_uuid
        FOREIGN KEY            (subnet_uuid)
        REFERENCES             subnet(uuid)
);

CREATE INDEX idx_availability_zone_subnet_availability_zone_uuid
ON availability_zone_subnet (uuid);

CREATE INDEX idx_availability_zone_subnet_subnet_uuid
ON availability_zone_subnet (subnet_uuid);

CREATE TABLE fan_network (
    uuid                TEXT PRIMARY KEY,
    local_underlay_cidr TEXT NOT NULL,
    overlay_cidr        TEXT NOT NULL
);

CREATE TABLE subnet (
    uuid                         TEXT PRIMARY KEY,
    cidr                         TEXT NOT NULL,
    vlan_tag                     INT,
    is_public                    BOOLEAN,
    space_uuid                   TEXT,
    fan_uuid                     TEXT,
    CONSTRAINT                   fk_subnets_spaces
        FOREIGN KEY                  (space_uuid)
        REFERENCES                   spaces(uuid)
    CONSTRAINT                   fk_subnets_fan_networks
        FOREIGN KEY                  (fan_uuid)
        REFERENCES                   fan_network(uuid)
);
`)
}
