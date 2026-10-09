#!/usr/bin/env python3
"""Persistent data and relation witness for controller recovery tests."""

import ops


class RecoveryWitness(ops.CharmBase):
    def __init__(self, framework):
        super().__init__(framework)
        framework.observe(self.on.workload_pebble_ready, self._ready)
        framework.observe(self.on.sink_relation_changed, self._relation)
        framework.observe(self.on.source_relation_changed, self._relation)
        framework.observe(self.on.leader_elected, self._leader)

    def _ready(self, event):
        event.workload.add_layer("witness", {
            "summary": "Recovery witness",
            "services": {"witness": {
                "override": "replace",
                "command": "sleep infinity",
                "startup": "enabled",
            }},
        }, combine=True)
        event.workload.replan()
        self.unit.status = ops.ActiveStatus()

    def _relation(self, event):
        self._publish(event.relation)

    def _publish(self, relation):
        if self.unit.is_leader():
            relation.data[self.app]["witness"] = self.model.uuid

    def _leader(self, _):
        for relations in self.model.relations.values():
            for relation in relations:
                self._publish(relation)


if __name__ == "__main__":
    ops.main(RecoveryWitness)
