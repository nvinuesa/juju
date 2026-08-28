# Restore-backup integration test: bootstrap a source controller with two
# models plus a CMR relation, take a backup, bootstrap a fresh target
# controller, stop the agent, restore, and verify source identity + models.
#
# This runs the same-machine target restore path (the v1 acceptance
# scenario): the restore binary is seeded into the target machine before
# the agent is stopped.
run_restore_cmr() {
	echo

	# Bootstrap source controller.
	file="${TEST_DIR}/test-restore-cmr.log"
	ensure "src-ctrl" "${file}"
	juju switch "src-ctrl"

	echo "Creating offer and consume models with CMR"
	juju add-model "model-offer" -c "src-ctrl"
	juju switch "src-ctrl:model-offer"
	juju deploy juju-qa-dummy-source --base ubuntu@22.04
	juju offer dummy-source:sink dummy-offer
	wait_for "dummy-source" "$(idle_condition "dummy-source")"

	juju add-model "model-consume" -c "src-ctrl"
	juju switch "src-ctrl:model-consume"
	juju deploy juju-qa-dummy-sink --base ubuntu@22.04
	wait_for "dummy-sink" "$(idle_condition "dummy-sink")"
	juju consume "src-ctrl:admin/model-offer.dummy-offer"
	juju relate dummy-sink dummy-offer
	wait_for "dummy-offer" '.applications["dummy-sink"] | .relations.source[0]'

	echo "Driving CMR config through the relation"
	juju switch "src-ctrl:model-offer"
	juju config dummy-source token=restore-demo

	echo "Creating backup"
	backup_file="${TEST_DIR}/source-backup.tar.gz"
	# Backup to an explicit target path (no peek at /var/lib/juju).
	juju switch "src-ctrl:admin"
	juju create-backup --no-download --metadata "restore integration"
	archive_path="$(juju run --machine 0 sudo ls -t /var/lib/juju/backups/*.tar.gz | head -1 | sed 's/machine-0: //')"
	[ -n "${archive_path}" ] || {
		echo "==> could not locate backup archive"
		exit 1
	}
	# scp the archive out of the source machine.
	juju scp -m "src-ctrl:controller" "0:${archive_path}" "${backup_file}"
	[ -s "${backup_file}" ] || {
		echo "==> backup scp returned no archive"
		exit 1
	}

	echo "Bootstrapping target controller and seeding restore binary"
	# The restore utility must be linked with Dqlite support.
	make -C .. restore-backup
	go_bin="$(go env GOBIN)"
	if [ -z "${go_bin}" ]; then
		go_bin="$(go env GOPATH)/bin"
	fi
	restore_binary="${go_bin}/restore-backup"
	juju bootstrap lxd "dst-ctrl" --config defaults-cmgt=ctrl --model-default cpu=1 --model-default mem=1G
	juju scp -m "dst-ctrl:controller" "${restore_binary}" "0:/tmp/restore-backup"
	juju scp -m "dst-ctrl:controller" "${backup_file}" "0:/tmp/source-backup.tar.gz"

	echo "Stopping target agent and running restore"
	juju ssh -m "dst-ctrl:controller" 0 sudo systemctl stop jujuagentd-machine-0.service
	juju ssh -m "dst-ctrl:controller" 0 sudo -i "JUJU_DATA_DIR=/var/lib/juju /tmp/restore-backup /tmp/source-backup.tar.gz" || {
		echo "==> restore failed"
		juju ssh -m "dst-ctrl:controller" 0 sudo -i "systemctl start jujuagentd-machine-0.service"
		exit 1
	}
	echo "Restarting the target agent and verifying restore"
	juju ssh -m "dst-ctrl:controller" 0 sudo -i "systemctl start jujuagentd-machine-0.service"

	# The restored controller serves the source controller UUID; the source
	# models are present with their source logical state.
	expected_src_ctrl_uuid="$(juju show-controller src-ctrl --format=json | yq -r '.src-ctrl.controller[\"uuid\"]')"
	got_ctrl_uuid="$(juju show-controller dst-ctrl --format=json | yq -r '.dst-ctrl.controller[\"uuid\"]')"
	if [ "${got_ctrl_uuid}" != "${expected_src_ctrl_uuid}" ]; then
		echo "==> expected source controller uuid ${expected_src_ctrl_uuid}, got ${got_ctrl_uuid}"
		exit 1
	fi

	echo "Verify model UUIDs present"
	for model in "model-offer" "model-consume"; do
		got="$(juju show-model --format=json "${model}" | yq -r '.model[\"uuid\"]')"
		src="$(juju show-model -c src-ctrl --format=json "${model}" | yq -r '.model[\"uuid\"]')"
		if [ "${got}" != "${src}" ]; then
			echo "==> expected model ${model} uuid ${src}, got ${got}"
			exit 1
		fi
	done

	echo "Verify CMR offer still holds its source relation"
	juju switch "dst-ctrl:model-offer"
	juju config dummy-source | grep -q 'token=restore-demo' ||
		{ echo "==> model-offer config token not preserved"; exit 1; }

	echo "Restore integration test passed"
}

test_restore_cmr() {
	if [ "$(skip 'test_restore_cmr')" ]; then
		echo "==> TEST SKIPPED: restore cmr"
		return
	fi

	(
		set_verbosity

		echo "==> Checking for dependencies"
		check_dependencies juju go make yq

		file="${TEST_DIR}/test-restore.log"

		bootstrap "test-restore" "${file}"

		run "run_restore_cmr"

		destroy_controller "test-restore"
	)
}
