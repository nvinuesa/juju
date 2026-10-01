test_backup_restore() {
	if [ "$(skip 'test_backup_restore')" ]; then
		echo "==> TEST SKIPPED: backup restore tests"
		return
	fi

	set_verbosity

	echo "==> Checking for dependencies"
	check_dependencies juju

	file="${TEST_DIR}/test-backup-restore.log"

	case "${BOOTSTRAP_PROVIDER:-lxd}" in
	"lxd")
		check_dependencies lxc
		bootstrap "test-backup-restore-iaas" "${file}"
		test_restore_iaas
		;;
	"microk8s")
		check_dependencies microk8s kubectl
		bootstrap "test-backup-restore-k8s" "${file}"
		test_restore_k8s
		;;
	*)
		echo "==> TEST SKIPPED: backup restore not supported for ${BOOTSTRAP_PROVIDER}"
		return
		;;
	esac
}
