# create_backup_on_controller creates a backup on the current controller;
# 4.1 streams the archive to the client in the same request and keeps
# nothing on the controller. Prints the local path and SHA-256 hex digest,
# separated by a tab.
create_backup_on_controller() {
	juju switch controller >&2 || return 1

	# Pin this client's connection to the selected source. In an HA cluster,
	# ordinary API connection selection could otherwise choose another node.
	local machine client_dir backup_client source_ip api_port
	machine=${1:-0}
	client_dir=${JUJU_DATA:-${HOME}/.local/share/juju}
	backup_client="${TEST_DIR}/recovery-client-${machine}"
	mkdir -p "${backup_client}" || return 1
	cp -a "${client_dir}/accounts.yaml" "${client_dir}/controllers.yaml" \
		"${client_dir}/models.yaml" "${backup_client}/" || return 1
	source_ip=$(juju show-machine "${machine}" --format json |
		machine="${machine}" yq -r '.machines[strenv(machine)]."ip-addresses"[0]') || return 1
	api_port=$(juju controller-config api-port) || return 1
	controller="${BOOTSTRAPPED_JUJU_CTRL_NAME}" endpoint="${source_ip}:${api_port}" yq -i \
		'.controllers[strenv(controller)]."api-endpoints" = [strenv(endpoint)]' \
		"${backup_client}/controllers.yaml" || return 1

	local output local_path sum recorded_machine
	local_path="${TEST_DIR}/recovery-archive.tar.gz"
	if ! output=$(JUJU_DATA="${backup_client}" juju create-backup --filename "${local_path}" 2>&1); then
		echo "==> create-backup failed: ${output}" >&2
		return 1
	fi

	sum=$(awk '$1 == "checksum:" {print $2}' <<<"${output}")
	sum=${sum#sha256-}
	if [[ ! ${sum} =~ ^[[:xdigit:]]{64}$ ]]; then
		echo "==> cannot parse create-backup checksum: ${output}" >&2
		return 1
	fi

	# The downloaded bytes must match the reported checksum before the
	# test trusts them.
	local local_sum
	local_sum=$(sha256sum "${local_path}") || return 1
	local_sum=${local_sum%% *}
	if [[ ${local_sum} != "${sum}" ]]; then
		echo "==> fetched archive checksum mismatch: ${local_sum} != ${sum}" >&2
		return 1
	fi
	recorded_machine=$(tar -xOf "${local_path}" juju-backup/metadata.json |
		yq -r '.ControllerMachineID') || return 1
	if [[ ${recorded_machine} != "${machine}" ]]; then
		echo "==> backup source machine ${recorded_machine} != ${machine}" >&2
		return 1
	fi

	printf '%s\t%s\n' "${local_path}" "${sum}"
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
	echo "${OUT}" | grep "agent for version" || {
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
	local machine container ip
	machine=${1:-0}
	container=$(juju show-machine "${machine}" -m controller --format json |
		machine="${machine}" yq -r '.machines[strenv(machine)]."instance-id"')
	if [[ -z ${container} ]]; then
		echo "==> cannot find the source controller instance" >&2
		exit 1
	fi
	ip=$(lxc list "${container}" --format csv -c 4 | awk '{print $1}' | head -1)
	lxc stop --force "${container}"
	lxc delete "${container}"
	echo "${ip}"
}

# Recover from a backup made on controller/1, retiring the other HA members.
test_recovery_nonzero_controller() {
	juju switch controller
	juju add-unit controller -n 2
	wait_for_controller_machines 3
	wait_for_ha 3
	local unit
	for unit in 0 1 2; do
		wait_for "controller" "$(idle_condition "controller" "${unit}")"
	done
	test_recovery_iaas 1
}

# test_recovery_iaas recovers an IAAS controller onto a fresh replacement.
test_recovery_iaas() {
	local controller_unit source_machine source_instance controller_instances instance
	controller_unit=${1:-0}
	source_machine=$(juju status -m controller --format json |
		unit="controller/${controller_unit}" yq -r '.applications.controller.units[strenv(unit)].machine')
	source_instance=$(juju show-machine "${source_machine}" -m controller --format json |
		machine="${source_machine}" yq -r '.machines[strenv(machine)]."instance-id"')
	controller_instances=$(juju machines -m controller --format json |
		yq -r '.machines[]."instance-id"')
	echo "==> deploying a workload to recover over"
	add_model workload
	juju deploy ubuntu
	wait_for "ubuntu" "$(idle_condition "ubuntu" 0)"

	local source_controller_uuid
	source_controller_uuid=$(juju show-controller --format json |
		yq -e -r ".${BOOTSTRAPPED_JUJU_CTRL_NAME}.details.uuid")

	local archive sum backup
	backup=$(create_backup_on_controller "${source_machine}") || return 1
	IFS=$'\t' read -r archive sum <<<"${backup}"

	local old_ip
	old_ip=$(fence_source_controller "${source_machine}")
	for instance in ${controller_instances}; do
		if [[ ${instance} != "${source_instance}" ]]; then
			lxc stop --force "${instance}"
			lxc delete "${instance}"
		fi
	done

	# The dead source's client-side registration carries the controller
	# UUID the replacement adopts; remove the record so the recovery
	# command can register the replacement under it. The recovery
	# command refuses a locally registered controller name before it
	# selects the archived agent, so the registration must go before
	# the preflight refusal checks.
	juju unregister --no-prompt "${BOOTSTRAPPED_JUJU_CTRL_NAME}"
	# The harness cleanup would otherwise try to destroy the already
	# unregistered controller again.
	sed -i "/^${BOOTSTRAPPED_JUJU_CTRL_NAME}$/d" "${TEST_DIR}/jujus" 2>/dev/null || true

	test_recovery_preflight_failures "${archive}" "${sum}"

	echo "==> recovering replacement from the archive"
	juju recovery "${archive}" --sha256 "${sum}"
	echo "${BOOTSTRAPPED_JUJU_CTRL_NAME}" >>"${TEST_DIR}/jujus"

	echo "==> checking recovered identities"
	local recovered_uuid
	recovered_uuid=$(juju show-controller "${BOOTSTRAPPED_JUJU_CTRL_NAME}" --format json |
		yq -e -r ".${BOOTSTRAPPED_JUJU_CTRL_NAME}.details.uuid")
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
	juju switch controller
	wait_for "controller" ".applications.controller | select((.units | keys) == [\"controller/${controller_unit}\"]) | \"controller\"" 900
	wait_for "controller" "$(idle_condition "controller" "${controller_unit}")" 900
	local new_ip new_container new_machine
	new_machine=$(juju status --format json |
		unit="controller/${controller_unit}" yq -r '.applications.controller.units[strenv(unit)].machine')
	new_container=$(juju show-machine "${new_machine}" --format json |
		machine="${new_machine}" yq -r '.machines[strenv(machine)]."instance-id"')
	new_ip=$(lxc list "${new_container}" --format csv -c 4 | awk '{print $1}' | head -1)
	echo "==> endpoint handoff: ${old_ip} -> ${new_ip}"
	local workload_uuid workload_container machine
	workload_uuid=$(juju show-model workload --format json | yq -r '.workload.model-uuid')
	for machine in $(lxc list --format csv -c n | grep "^juju-$(echo "${workload_uuid}" | tail -c 7)"); do
		workload_container=${machine}
		lxc exec "${workload_container}" -- bash -c \
			"sed -i 's/${old_ip}/${new_ip}/g' /var/lib/juju/agents/*/agent.conf && systemctl restart jujuagentd-machine-*"
	done

	echo "==> waiting for the workload agent to reconnect"
	juju switch workload
	wait_for "ubuntu" "$(idle_condition "ubuntu" 0)" 900

	# A unit deployed after recovery must download its charm from the
	# recovered controller, which restored the blob from the archive.
	echo "==> deploying a new application on the recovered controller"
	juju deploy ubuntu probe
	wait_for "probe" "$(idle_condition "probe" 0)" 900

	juju create-backup --filename "${TEST_DIR}/after-recovery.tar.gz" >/dev/null
	destroy_model "workload"
	juju switch controller
}
