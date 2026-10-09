test_backup_recovery() {
	if [ "$(skip 'test_backup_recovery')" ]; then
		echo "==> TEST SKIPPED: backup recovery tests"
		return
	fi

	set_verbosity

	echo "==> Checking for dependencies"
	check_dependencies juju

	file="${TEST_DIR}/test-backup-recovery.log"

	case "${BOOTSTRAP_PROVIDER:-lxd}" in
	"lxd")
		check_dependencies lxc
		bootstrap "test-backup-recovery-iaas" "${file}"
		test_recovery_iaas
		;;
	"microk8s")
		check_dependencies microk8s kubectl
		bootstrap "test-backup-recovery-k8s-$(rnd_str)" "${file}"
		test_recovery_k8s
		;;
	*)
		echo "==> TEST SKIPPED: backup recovery not supported for ${BOOTSTRAP_PROVIDER}"
		return
		;;
	esac
}
