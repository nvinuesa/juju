# create_backup_on_controller creates a backup on the current controller;
# 4.1 streams the archive to the client in the same request and keeps
# nothing on the controller. Prints "<local-path> <sha256hex>".
create_backup_on_controller() {
	juju switch controller

	OUT=$(juju create-backup --filename "${TEST_DIR}/recovery-archive.tar.gz" 2>&1)
	echo "${OUT}" | grep -qi "backup" || {
		echo "==> create-backup failed: ${OUT}"
		exit 1
	}

	local local_path sum
	local_path=$(echo "${OUT}" | grep "^Downloaded to" | awk '{print $3}')
	sum=$(echo "${OUT}" | grep "^checksum:" | awk '{print $2}')
	if [[ -z ${local_path} || -z ${sum} ]]; then
		echo "==> cannot parse create-backup output: ${OUT}"
		exit 1
	fi

	# The downloaded bytes must match the reported checksum before the
	# test trusts them.
	local local_sum
	local_sum=$(sha256sum "${local_path}" | awk '{print $1}')
	if [[ ${local_sum} != "${sum}" ]]; then
		echo "==> fetched archive checksum mismatch: ${local_sum} != ${sum}"
		exit 1
	fi

	echo "${local_path} ${sum}"
}

# test_recovery_preflight_failures checks that invalid archives are refused
# client-side before anything is provisioned. Needs an archive path and
# its checksum as produced by create_backup_on_controller.
test_recovery_preflight_failures() {
	local archive sum
	archive=${1}
	sum=${2}

	echo "==> recovery preflight: checksum mismatch refused"
	OUT=$(juju recovery "${archive}" \
		--sha256 0000000000000000000000000000000000000000000000000000000000000000 2>&1 || true)
	echo "${OUT}" | grep "checksum mismatch" || {
		echo "==> expected checksum mismatch refusal, got: ${OUT}"
		exit 1
	}

	echo "==> recovery preflight: agent version mismatch refused"
	local repack="${TEST_DIR}/repack"
	rm -rf "${repack}" && mkdir -p "${repack}"
	tar -xzf "${archive}" -C "${repack}"
	sed -i 's/"Version":"[^"]*"/"Version":"1.2.3"/' "${repack}/juju-backup/metadata.json"
	# Re-sign the changed metadata entry in the content manifest so the
	# archive reaches the version gate rather than failing integrity checks.
	local metadata_sum metadata_size
	metadata_sum=$(sha256sum "${repack}/juju-backup/metadata.json" | awk '{print $1}')
	metadata_size=$(wc -c <"${repack}/juju-backup/metadata.json")
	metadata_sum="${metadata_sum}" metadata_size="${metadata_size}" yq -o=json -i \
		'(.files[] | select(.path == "juju-backup/metadata.json")).sha256 = strenv(metadata_sum) |
		 (.files[] | select(.path == "juju-backup/metadata.json")).size = env(metadata_size)' \
		"${repack}/juju-backup/manifest.json"
	local repacked="${TEST_DIR}/recovery-repacked.tar.gz"
	tar -czf "${repacked}" -C "${repack}" juju-backup
	local repacked_sum
	repacked_sum=$(sha256sum "${repacked}" | awk '{print $1}')
	OUT=$(juju recovery "${repacked}" \
		--sha256 "${repacked_sum}" 2>&1 || true)
	echo "${OUT}" | grep "agent version" || {
		echo "==> expected version mismatch refusal, got: ${OUT}"
		exit 1
	}

	echo "==> recovery preflight failures refused cleanly"
}

# fence_source_controller stops and removes the source controller machine
# on LXD without touching workload machines, simulating controller loss.
# Prints the source controller's machine container IP for the endpoint
# handoff.
fence_source_controller() {
	local short_uuid container ip
	short_uuid=$(juju show-model controller --format json | yq -r '.controller.model-uuid' | tail -c 7)
	container=$(lxc list --format csv -c n | grep "^juju-${short_uuid}-0" | head -1 || true)
	if [[ -z ${container} ]]; then
		echo "==> cannot find source controller container (juju-${short_uuid}-0)" >&2
		exit 1
	fi
	ip=$(lxc list "${container}" --format csv -c 4 | awk '{print $1}' | head -1)
	lxc stop --force "${container}"
	lxc delete "${container}"
	echo "${ip}"
}

# test_recovery_iaas recovers an IAAS controller onto a fresh replacement.
test_recovery_iaas() {
	echo "==> deploying a workload to recover over"
	juju add-model workload
	juju deploy ubuntu
	wait_for "ubuntu" "$(idle_condition "ubuntu" 0)"

	local source_controller_uuid
	source_controller_uuid=$(juju show-controller --format json |
		yq -r ".${BOOTSTRAPPED_JUJU_CTRL_NAME}.uuid")

	read -r archive sum <<<"$(create_backup_on_controller)"

	test_recovery_preflight_failures "${archive}" "${sum}"

	local old_ip
	old_ip=$(fence_source_controller)

	# The dead source's client-side registration carries the controller
	# UUID the replacement adopts; remove the record so the recovery
	# command can register the replacement under it.
	juju unregister --no-prompt "${BOOTSTRAPPED_JUJU_CTRL_NAME}"
	# The harness cleanup would otherwise try to destroy the already
	# unregistered controller again.
	sed -i "/^${BOOTSTRAPPED_JUJU_CTRL_NAME}$/d" "${TEST_DIR}/jujus" 2>/dev/null || true

	echo "==> recovering replacement from the archive"
	juju recovery "${archive}" --sha256 "${sum}"

	echo "==> checking recovered identities"
	local recovered_uuid
	recovered_uuid=$(juju show-controller "${BOOTSTRAPPED_JUJU_CTRL_NAME}" --format json |
		yq -r ".${BOOTSTRAPPED_JUJU_CTRL_NAME}.uuid")
	if [[ ${recovered_uuid} != "${source_controller_uuid}" ]]; then
		echo "==> controller UUID not preserved: ${recovered_uuid} != ${source_controller_uuid}"
		exit 1
	fi
	juju models --format json | yq -e '.models[] | select((.name | sub("^admin/"; "")) == "workload")' >/dev/null || {
		echo "==> workload model missing after recovery"
		exit 1
	}
	juju status -m workload --format json | yq -e '.applications.ubuntu.units["ubuntu/0"]' >/dev/null || {
		echo "==> ubuntu/0 missing after recovery"
		exit 1
	}

	# Endpoint handoff (operator-owned): point the surviving workload
	# agents at the replacement's address and restart them.
	local new_ip new_container short_uuid
	short_uuid=$(juju show-model controller --format json | yq -r '.controller.model-uuid' | tail -c 7)
	new_container=$(lxc list --format csv -c n | grep "^juju-${short_uuid}-0" | head -1 || true)
	new_ip=$(lxc list "${new_container}" --format csv -c 4 | awk '{print $1}' | head -1)
	echo "==> endpoint handoff: ${old_ip} -> ${new_ip}"
	local workload_uuid workload_container machine
	workload_uuid=$(juju show-model workload --format json | yq -r '.workload.model-uuid')
	for machine in $(lxc list --format csv -c n | grep "^juju-$(echo "${workload_uuid}" | tail -c 7)"); do
		workload_container=${machine}
		lxc exec "${workload_container}" -- bash -c \
			"sed -i 's/${old_ip}/${new_ip}/g' /var/lib/juju/agents/*/agent.conf && systemctl restart jujuagentd-machine-* jujuagentd-unit-*"
	done

	echo "==> waiting for the workload agent to reconnect"
	juju switch workload
	wait_for "ubuntu" "$(idle_condition "ubuntu" 0)" 900

	juju create-backup --filename "${TEST_DIR}/after-recovery.tar.gz" >/dev/null
	destroy_controller "${BOOTSTRAPPED_JUJU_CTRL_NAME}"
}
