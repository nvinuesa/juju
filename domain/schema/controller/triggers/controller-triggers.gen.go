// Code generated by triggergen. DO NOT EDIT.

package triggers

import (
	"fmt"

	"github.com/juju/juju/core/database/schema"
)


// ChangeLogTriggersForControllerConfig generates the triggers for the 
// controller_config table.
func ChangeLogTriggersForControllerConfig(columnName string, namespaceID int) func() schema.Patch {
	return func() schema.Patch {
		return schema.MakePatch(fmt.Sprintf(`
-- insert trigger for ControllerConfig
CREATE TRIGGER trg_log_controller_config_insert
AFTER INSERT ON controller_config FOR EACH ROW
BEGIN
    INSERT INTO change_log (edit_type_id, namespace_id, changed, created_at)
    VALUES (1, %[2]d, NEW.%[1]s, DATETIME('now'));
END;

-- update trigger for ControllerConfig
CREATE TRIGGER trg_log_controller_config_update
AFTER UPDATE ON controller_config FOR EACH ROW
WHEN 
	(NEW.value != OLD.value OR (NEW.value IS NOT NULL AND OLD.value IS NULL) OR (NEW.value IS NULL AND OLD.value IS NOT NULL)) 
BEGIN
    INSERT INTO change_log (edit_type_id, namespace_id, changed, created_at)
    VALUES (2, %[2]d, OLD.%[1]s, DATETIME('now'));
END;

-- delete trigger for ControllerConfig
CREATE TRIGGER trg_log_controller_config_delete
AFTER DELETE ON controller_config FOR EACH ROW
BEGIN
    INSERT INTO change_log (edit_type_id, namespace_id, changed, created_at)
    VALUES (4, %[2]d, OLD.%[1]s, DATETIME('now'));
END;`, columnName, namespaceID))
	}
}

// ChangeLogTriggersForControllerNode generates the triggers for the 
// controller_node table.
func ChangeLogTriggersForControllerNode(columnName string, namespaceID int) func() schema.Patch {
	return func() schema.Patch {
		return schema.MakePatch(fmt.Sprintf(`
-- insert trigger for ControllerNode
CREATE TRIGGER trg_log_controller_node_insert
AFTER INSERT ON controller_node FOR EACH ROW
BEGIN
    INSERT INTO change_log (edit_type_id, namespace_id, changed, created_at)
    VALUES (1, %[2]d, NEW.%[1]s, DATETIME('now'));
END;

-- update trigger for ControllerNode
CREATE TRIGGER trg_log_controller_node_update
AFTER UPDATE ON controller_node FOR EACH ROW
WHEN 
	(NEW.dqlite_node_id != OLD.dqlite_node_id OR (NEW.dqlite_node_id IS NOT NULL AND OLD.dqlite_node_id IS NULL) OR (NEW.dqlite_node_id IS NULL AND OLD.dqlite_node_id IS NOT NULL)) OR
	(NEW.bind_address != OLD.bind_address OR (NEW.bind_address IS NOT NULL AND OLD.bind_address IS NULL) OR (NEW.bind_address IS NULL AND OLD.bind_address IS NOT NULL)) 
BEGIN
    INSERT INTO change_log (edit_type_id, namespace_id, changed, created_at)
    VALUES (2, %[2]d, OLD.%[1]s, DATETIME('now'));
END;

-- delete trigger for ControllerNode
CREATE TRIGGER trg_log_controller_node_delete
AFTER DELETE ON controller_node FOR EACH ROW
BEGIN
    INSERT INTO change_log (edit_type_id, namespace_id, changed, created_at)
    VALUES (4, %[2]d, OLD.%[1]s, DATETIME('now'));
END;`, columnName, namespaceID))
	}
}

