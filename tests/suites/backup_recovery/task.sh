test_backup_recovery() {
	if [ "$(skip 'test_backup_recovery')" ]; then
		echo "==> TEST SKIPPED: backup recovery tests"
		return
	fi

	set_verbosity

	echo "==> Checking for dependencies"
	check_dependencies juju

	case "${BOOTSTRAP_PROVIDER:-lxd}" in
	"lxd")
		check_dependencies lxc
		;;
	"microk8s")
		check_dependencies microk8s kubectl
		;;
	"k8s")
		check_dependencies kubectl
		;;
	*)
		echo "==> TEST SKIPPED: backup recovery not supported for ${BOOTSTRAP_PROVIDER}"
		return
		;;
	esac

	file="${TEST_DIR}/test-backup-recovery.log"

	bootstrap "test-backup-recovery" "${file}"

	case "${BOOTSTRAP_PROVIDER:-lxd}" in
	"lxd")
		test_recovery_iaas

		# The HA recovery needs a pristine source controller, not the
		# replacement recovered above.
		destroy_controller "${BOOTSTRAPPED_JUJU_CTRL_NAME}"
		file="${TEST_DIR}/test-backup-recovery-ha.log"
		bootstrap "test-backup-recovery" "${file}"

		test_recovery_nonzero_controller
		;;
	"microk8s" | "k8s")
		test_recovery_k8s
		;;
	esac

	destroy_controller "${BOOTSTRAPPED_JUJU_CTRL_NAME}"
}
