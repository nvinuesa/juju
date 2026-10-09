# create_backup_on_k8s_controller creates a backup on the current
# Kubernetes controller; 4.1 streams the archive to the client in the
# same request. Prints "<local-path> <sha256hex>".
create_backup_on_k8s_controller() {
	local controller_name namespace
	controller_name=${1}
	namespace="controller-${controller_name}"

	juju switch controller

	OUT=$(juju create-backup --filename "${TEST_DIR}/recovery-archive-k8s.tar.gz" 2>&1)
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

# test_recovery_k8s recovers a Kubernetes controller onto the same
# cluster, reusing the source controller name so the fresh controller
# namespace keeps its name.
test_recovery_k8s() {
	local controller_name namespace workload_model
	controller_name="${BOOTSTRAPPED_JUJU_CTRL_NAME}"
	namespace="controller-${controller_name}"
	workload_model="${controller_name}-workload"

	echo "==> adding a workload model to recover over"
	juju add-model "${workload_model}"

	local source_controller_uuid
	source_controller_uuid=$(juju show-controller --format json |
		yq -r ".${controller_name}.uuid")

	read -r archive sum <<<"$(create_backup_on_k8s_controller "${controller_name}")"

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

	echo "==> recovery preflight: wrong controller name refused"
	OUT=$(juju recovery "${archive}" test-k8s-wrong-name \
		--sha256 "${sum}" 2>&1 || true)
	echo "${OUT}" | grep "unrecognized args" || {
		echo "==> expected controller name refusal, got: ${OUT}"
		exit 1
	}
	if juju controllers 2>/dev/null | grep -q test-k8s-wrong-name; then
		echo "==> refused recovery registered a controller"
		exit 1
	fi
	echo "==> wrong name refused cleanly"

	echo "==> fencing the source controller"
	kubectl scale statefulset controller -n "${namespace}" --replicas=0
	kubectl delete pods --all -n "${namespace}" --grace-period=0 --force
	kubectl delete ns "${namespace}" --wait=true

	echo "==> recovering replacement from the archive"
	juju recovery "${archive}" --sha256 "${sum}"

	echo "==> checking recovered identities"
	local recovered_uuid
	recovered_uuid=$(juju show-controller "${controller_name}" --format json |
		yq -r ".${controller_name}.uuid")
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

	juju create-backup --filename "${TEST_DIR}/after-recovery-k8s.tar.gz" >/dev/null
	destroy_controller "${controller_name}"
}
