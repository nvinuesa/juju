# create_backup_on_k8s_controller creates a backup on the current
# Kubernetes controller; 4.1 streams the archive to the client in the
# same request. Prints "<local-path> <sha256hex>".
create_backup_on_k8s_controller() {
	local controller_name namespace
	controller_name=${1}
	namespace="controller-${controller_name}"

	juju switch controller

	OUT=$(juju create-backup --filename "${TEST_DIR}/restore-archive-k8s.tar.gz" 2>&1)
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

# test_restore_k8s restores a Kubernetes controller onto the same
# cluster, reusing the source controller name so the fresh controller
# namespace keeps its name.
test_restore_k8s() {
	local controller_name namespace
	controller_name="${BOOTSTRAPPED_JUJU_CTRL_NAME}"
	namespace="controller-${controller_name}"

	echo "==> adding a workload model to restore over"
	juju add-model workload-k8s

	local source_controller_uuid
	source_controller_uuid=$(juju show-controller --format json |
		yq -r ".${controller_name}.uuid")

	read -r archive sum <<<"$(create_backup_on_k8s_controller "${controller_name}")"

	# The replacement adopts the source's controller name and UUID, so
	# the dead source's client-side registration must go before any
	# restore bootstrap can run (the CLI refuses an already registered
	# controller name). The source itself keeps running until the
	# fencing step below.
	juju unregister --no-prompt "${controller_name}"

	echo "==> restore preflight: unfenced source refused"
	# The source controller is still running under its own name: the
	# substrate check must refuse the restore before anything is
	# provisioned.
	OUT=$(juju bootstrap microk8s "${controller_name}" --restore "${archive}" \
		--restore-sha256 "${sum}" 2>&1 || true)
	echo "${OUT}" | grep "not fenced" || {
		echo "==> expected unfenced-source refusal, got: ${OUT}"
		exit 1
	}
	# The refusal must not have touched the source.
	local replicas
	replicas=$(kubectl get statefulset controller -n "${namespace}" -o json |
		yq -r '.spec.replicas')
	if [[ ${replicas} != "1" ]]; then
		echo "==> refused restore modified the source statefulset (replicas=${replicas})"
		exit 1
	fi
	echo "==> unfenced source refused cleanly"

	echo "==> restore preflight: wrong controller name refused"
	OUT=$(juju bootstrap microk8s test-k8s-wrong-name --restore "${archive}" \
		--restore-sha256 "${sum}" 2>&1 || true)
	echo "${OUT}" | grep "requires the source controller name" || {
		echo "==> expected controller name refusal, got: ${OUT}"
		exit 1
	}
	if juju controllers 2>/dev/null | grep -q test-k8s-wrong-name; then
		echo "==> refused bootstrap registered a controller"
		exit 1
	fi
	echo "==> wrong name refused cleanly"

	echo "==> fencing the source controller"
	kubectl scale statefulset controller -n "${namespace}" --replicas=0
	kubectl delete ns "${namespace}" --wait=true

	echo "==> bootstrapping replacement with --restore"
	juju bootstrap microk8s "${controller_name}" \
		--restore "${archive}" --restore-sha256 "${sum}"

	echo "==> checking restored identities"
	local restored_uuid
	restored_uuid=$(juju show-controller "${controller_name}" --format json |
		yq -r ".${controller_name}.uuid")
	if [[ ${restored_uuid} != "${source_controller_uuid}" ]]; then
		echo "==> controller UUID not preserved: ${restored_uuid} != ${source_controller_uuid}"
		exit 1
	fi
	juju models --format json | yq -e '.models[] | select((.name | sub("^admin/"; "")) == "workload-k8s")' >/dev/null || {
		echo "==> workload-k8s model missing after restore"
		exit 1
	}

	echo "==> checking the surviving workload namespace was untouched"
	kubectl get ns workload-k8s -o json |
		ctrl="${source_controller_uuid}" yq -e \
			'.metadata.annotations["controller.juju.is/id"] == strenv(ctrl)' >/dev/null || {
		echo "==> workload namespace lost its controller annotation"
		exit 1
	}

	destroy_controller "${controller_name}"
}
