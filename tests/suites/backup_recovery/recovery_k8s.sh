# create_backup_on_k8s_controller creates a backup on the current
# Kubernetes controller; 4.1 streams the archive to the client in the
# same request. Prints the local path and SHA-256 hex digest, separated by a tab.
create_backup_on_k8s_controller() {
	local controller_name namespace
	controller_name=${1}
	namespace="controller-${controller_name}"

	juju switch controller >&2 || return 1

	local output local_path sum
	local_path="${TEST_DIR}/recovery-archive-k8s.tar.gz"
	if ! output=$(juju create-backup --filename "${local_path}" 2>&1); then
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

	printf '%s\t%s\n' "${local_path}" "${sum}"
}

# prepare_recovery_k8s_workload records persistent data, a secret and a
# relation before the backup. The same fixture can run on another cluster.
prepare_recovery_k8s_workload() {
	local model charm secret
	model=${1}
	charm=${2}
	juju switch "${model}"
	juju deploy "${charm}" witness --storage data=1G \
		--resource ubuntu-image=public.ecr.aws/ubuntu/ubuntu:24.04
	juju deploy "${charm}" peer --storage data=1G \
		--resource ubuntu-image=public.ecr.aws/ubuntu/ubuntu:24.04
	juju integrate witness:sink peer:source
	wait_for "witness" "$(active_idle_condition "witness" 0)"
	wait_for "peer" "$(active_idle_condition "peer" 0)"
	juju ssh --container workload witness/0 -- \
		"printf '%s' '${model}' > /data/recovery-witness"
	secret=$(juju add-secret recovery-witness "value=${model}")
	echo "${secret}" > "${TEST_DIR}/${model}-secret"
	juju show-secret "${secret}" --reveal --format yaml |
		yq -e -o=json -I=0 '.[].content' > "${TEST_DIR}/${model}-secret-data"
	juju show-model "${model}" --format json |
		model="${model}" yq -r '.[strenv(model)]."model-uuid"' > "${TEST_DIR}/${model}-uuid"
	juju storage --format json |
		yq -o=json -I=0 '[.. | select(tag == "!!map" and has("provider-id")) | ."provider-id"] | sort' > "${TEST_DIR}/${model}-storage"
	juju show-unit witness/0 --format json |
		yq -o=json -I=0 '.["witness/0"]."relation-info"[] | select(.endpoint == "sink") | ."application-data"' > "${TEST_DIR}/${model}-relation"
}

# check_recovery_k8s_workload verifies the original identities, provider
# storage records, on-volume bytes, secret value and relation application data.
check_recovery_k8s_workload() {
	local model secret actual
	model=${1}
	juju switch "${model}"
	wait_for "witness" "$(active_idle_condition "witness" 0)" 900
	wait_for "peer" "$(active_idle_condition "peer" 0)" 900
	# Unit agents may reconnect before the model operator has rediscovered
	# their pods. SSH needs the refreshed pod record as well as an idle agent.
	wait_for "witness" '.applications | to_entries[] | select((.value.units["witness/0"].address // "") != "") | .key' 900
	wait_for "peer" '.applications | to_entries[] | select((.value.units["peer/0"].address // "") != "") | .key' 900
	actual=$(juju ssh --container workload witness/0 -- cat /data/recovery-witness)
	[[ ${actual} == "${model}" ]] || { echo "==> recovered volume data changed"; exit 1; }
	secret=$(cat "${TEST_DIR}/${model}-secret")
	juju show-secret "${secret}" --reveal --format yaml |
		yq -e -o=json -I=0 '.[].content' > "${TEST_DIR}/${model}-secret-data-after"
	juju show-model "${model}" --format json |
		model="${model}" yq -r '.[strenv(model)]."model-uuid"' > "${TEST_DIR}/${model}-uuid-after"
	juju storage --format json |
		yq -o=json -I=0 '[.. | select(tag == "!!map" and has("provider-id")) | ."provider-id"] | sort' > "${TEST_DIR}/${model}-storage-after"
	juju show-unit witness/0 --format json |
		yq -o=json -I=0 '.["witness/0"]."relation-info"[] | select(.endpoint == "sink") | ."application-data"' > "${TEST_DIR}/${model}-relation-after"
	for actual in secret-data uuid storage relation; do
		cmp "${TEST_DIR}/${model}-${actual}" "${TEST_DIR}/${model}-${actual}-after"
	done
}

# handoff_recovery_k8s_endpoint routes the old internal API endpoint to the
# replacement so surviving agents can reconnect and learn its new addresses.
handoff_recovery_k8s_endpoint() {
	local namespace source_ip replacement_ip
	namespace=${1}
	source_ip=${2}
	replacement_ip=$(kubectl get service controller-service -n "${namespace}" -o json |
		yq -e -r '.spec.clusterIP')
	if [[ ${source_ip} == "${replacement_ip}" ]]; then
		return
	fi
	echo "==> endpoint handoff: ${source_ip} -> ${replacement_ip}"
	kubectl get service controller-service -n "${namespace}" -o json |
		source_ip="${source_ip}" yq -o=json '{
			"apiVersion": "v1",
			"kind": "Service",
			"metadata": {
				"name": "recovery-endpoint-handoff",
				"namespace": .metadata.namespace
			},
			"spec": {
				"type": "ClusterIP",
				"clusterIP": strenv(source_ip),
				"selector": .spec.selector,
				"ports": (.spec.ports | map(del(.nodePort)))
			}
		}' | kubectl create -f -
}

# destroy_recovery_k8s_model tears down a recovered workload model before
# the harness destroys the controller, keeping model removal on a live
# controller. Removal is forced (E2E-018): on Kubernetes, unit removal
# orphans dying filesystem attachments — their host net node is gone, so
# the storage provisioner never marks them dead and an unforced destroy
# hangs on them.
destroy_recovery_k8s_model() {
	local name=${1}
	echo "==> destroying workload model ${name} (forced)"
	if ! timeout "${DESTROY_TIMEOUT}" juju destroy-model --force --no-prompt --destroy-storage "${name}"; then
		echo "==> failed to destroy workload model ${name}"
		exit 1
	fi
}

# test_recovery_k8s recovers a Kubernetes controller onto the same
# cluster, reusing the source controller name so the fresh controller
# namespace keeps its name.
test_recovery_k8s() {
	local controller_name namespace workload_model charm secondary_model
	controller_name="${BOOTSTRAPPED_JUJU_CTRL_NAME}"
	namespace="controller-${controller_name}"
	workload_model="${controller_name}-workload"

	juju switch controller
	wait_for "controller" "$(idle_condition "controller" 0)"

	echo "==> adding a workload model to recover over"
	add_model "${workload_model}"
	charm=$(pack_charm "${CURRENT_DIR}/../testcharms/charms/recovery-k8s")
	prepare_recovery_k8s_workload "${workload_model}" "${charm}"
	# Set this to a registered, independently reachable Kubernetes cloud to
	# exercise per-model inventory and credential selection on two clusters.
	if [[ -n ${RECOVERY_SECONDARY_CLOUD:-} ]]; then
		secondary_model="${controller_name}-secondary"
		juju add-model "${secondary_model}" "${RECOVERY_SECONDARY_CLOUD}"
		add_client_ssh_key_to_juju_model "${secondary_model}"
		prepare_recovery_k8s_workload "${secondary_model}" "${charm}"
	fi
	juju switch controller

	local source_controller_uuid source_service_ip
	source_controller_uuid=$(juju show-controller --format json |
		yq -e -r ".${controller_name}.details.uuid")
	source_service_ip=$(kubectl get service controller-service -n "${namespace}" -o json |
		yq -e -r '.spec.clusterIP')

	# Record the actual source pod references, not the StatefulSet template.
	local source_agent_image source_charm_image metadata_agent_image metadata_charm_image
	source_agent_image=$(kubectl get pod controller-0 -n "${namespace}" -o json |
		yq -e -r '.spec.containers[] | select(.name == "api-server") | .image')
	source_charm_image=$(kubectl get pod controller-0 -n "${namespace}" -o json |
		yq -e -r '.spec.containers[] | select(.name == "charm") | .image')
	if [[ -z ${source_charm_image} || ${source_agent_image} == *@* || ${source_agent_image##*/} != *:* ]]; then
		echo "==> expected a tagged local agent image and a controller charm image"
		exit 1
	fi

	local archive sum backup
	backup=$(create_backup_on_k8s_controller "${controller_name}") || return 1
	IFS=$'\t' read -r archive sum <<<"${backup}"
	metadata_agent_image=$(tar -xOf "${archive}" juju-backup/metadata.json |
		yq -e -r '.Kubernetes.AgentImage')
	metadata_charm_image=$(tar -xOf "${archive}" juju-backup/metadata.json |
		yq -e -r '.Kubernetes.CharmImage')
	if [[ ${metadata_agent_image} != "${source_agent_image}" || ${metadata_charm_image} != "${source_charm_image}" ]]; then
		echo "==> backup image references do not match the source pod"
		exit 1
	fi

	# The replacement adopts the source's controller name and UUID, so
	# the dead source's client-side registration must go before any
	# recovery command can run (the CLI refuses an already registered
	# controller name). The source itself keeps running until the
	# fencing step below.
	juju unregister --no-prompt "${controller_name}"

	echo "==> recovery preflight: unfenced source refused"
	# The source controller is still running under its own name: the
	# substrate check must refuse the recovery before anything is
	# provisioned.
	OUT=$(juju recovery "${archive}" \
		--sha256 "${sum}" 2>&1 || true)
	echo "${OUT}" | grep "not fenced" || {
		echo "==> expected unfenced-source refusal, got: ${OUT}"
		exit 1
	}
	# The refusal must not have touched the source.
	local replicas
	replicas=$(kubectl get statefulset controller -n "${namespace}" -o json |
		yq -r '.spec.replicas')
	if [[ ${replicas} != "1" ]]; then
		echo "==> refused recovery modified the source statefulset (replicas=${replicas})"
		exit 1
	fi
	echo "==> unfenced source refused cleanly"

	# Recovery accepts only the archive path and uses its recorded name.
	echo "==> recovery CLI: extra controller-name argument refused"
	OUT=$(juju recovery "${archive}" test-k8s-wrong-name \
		--sha256 "${sum}" 2>&1 || true)
	echo "${OUT}" | grep "unrecognized args" || {
		echo "==> expected extra argument refusal, got: ${OUT}"
		exit 1
	}
	if juju controllers 2>/dev/null | grep -q test-k8s-wrong-name; then
		echo "==> refused recovery registered a controller"
		exit 1
	fi
	echo "==> extra controller-name argument refused cleanly"

	echo "==> fencing the source controller"
	kubectl scale statefulset controller -n "${namespace}" --replicas=0
	kubectl delete pods --all -n "${namespace}" --grace-period=0 --force
	kubectl delete ns "${namespace}" --wait=true

	echo "==> recovering replacement from the archive"
	juju recovery "${archive}" --sha256 "${sum}"
	handoff_recovery_k8s_endpoint "${namespace}" "${source_service_ip}"

	# The replacement's pod can still drop API connections while it
	# finishes starting; wait for it to answer and settle before reading
	# recovered state.
	juju switch controller
	wait_for "controller" "$(idle_condition "controller" 0)" 900

	echo "==> checking recovered identities"
	local recovered_uuid
	recovered_uuid=$(juju show-controller "${controller_name}" --format json |
		yq -e -r ".${controller_name}.details.uuid")
	if [[ ${recovered_uuid} != "${source_controller_uuid}" ]]; then
		echo "==> controller UUID not preserved: ${recovered_uuid} != ${source_controller_uuid}"
		exit 1
	fi
	juju models --format json | workload_model="${workload_model}" yq -e \
		'.models[] | select((.name | sub("^admin/"; "")) == strenv(workload_model))' >/dev/null || {
		echo "==> ${workload_model} model missing after recovery"
		exit 1
	}

	echo "==> checking the surviving workload namespace was untouched"
	kubectl get ns "${workload_model}" -o json |
		ctrl="${source_controller_uuid}" yq -e \
			'.metadata.annotations["controller.juju.is/id"] == strenv(ctrl)' >/dev/null || {
		echo "==> workload namespace lost its controller annotation"
		exit 1
	}
	check_recovery_k8s_workload "${workload_model}"
	if [[ -n ${secondary_model:-} ]]; then
		check_recovery_k8s_workload "${secondary_model}"
	fi
	juju switch controller

	echo "==> restarting the recovered controller pod"
	local recovered_pod_uid restarted_pod_uid
	recovered_pod_uid=$(kubectl get pod controller-0 -n "${namespace}" -o json |
		yq -e -r '.metadata.uid')
	kubectl delete pod controller-0 -n "${namespace}" --wait=true
	kubectl wait --for=create pod/controller-0 -n "${namespace}" --timeout=900s
	kubectl wait --for=condition=Ready pod/controller-0 -n "${namespace}" --timeout=900s
	restarted_pod_uid=$(kubectl get pod controller-0 -n "${namespace}" -o json |
		yq -e -r '.metadata.uid')
	if [[ -z ${restarted_pod_uid} || ${restarted_pod_uid} == "${recovered_pod_uid}" ]]; then
		echo "==> controller pod was not replaced"
		exit 1
	fi
	wait_for "controller" "$(idle_condition "controller" 0)" 900
	recovered_uuid=$(juju show-controller "${controller_name}" --format json |
		yq -e -r ".${controller_name}.details.uuid")
	if [[ ${recovered_uuid} != "${source_controller_uuid}" ]]; then
		echo "==> controller UUID changed after restart"
		exit 1
	fi
	check_recovery_k8s_workload "${workload_model}"
	if [[ -n ${secondary_model:-} ]]; then
		check_recovery_k8s_workload "${secondary_model}"
	fi
	juju switch controller

	juju create-backup --filename "${TEST_DIR}/after-recovery-k8s.tar.gz" >/dev/null
	destroy_recovery_k8s_model "${workload_model}"
	if [[ -n ${secondary_model:-} ]]; then
		destroy_recovery_k8s_model "${secondary_model}"
	fi
}
