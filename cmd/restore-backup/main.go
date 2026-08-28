// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/juju/juju/internal/errors"
	internallogger "github.com/juju/juju/internal/logger"
	"github.com/juju/juju/internal/restore"
)

func main() {
	os.Exit(Main(os.Args[1:]))
}

// usage is the restore-backup command line contract.
const usage = "usage: restore-backup <archive.tar.gz>"

// runtime opens the reduced-runtime Dqlite access on the stopped controller.
// The standalone jujud packaging supplies the concrete implementation; this
// interface is the stable seam the launcher satisfies.
type runtime interface {
	DBGetter() restore.DBGetter
	DBDeleter() restore.DBDeleter
	Close() error
}

// Main runs the restore-backup command and returns the process exit code.
func Main(args []string) int {
	ctx := context.Background()

	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	archivePath, err := filepath.Abs(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore-backup: resolving archive path: %v\n", err)
		return 2
	}

	rt, err := newRuntime(filepath.Dir(defaultObjectStoreRoot()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "restore-backup: starting reduced runtime: %v\n", err)
		return 1
	}

	objectStoreRoot := defaultObjectStoreRoot()
	logger := internallogger.GetLogger("juju.restore")
	r := &restore.Restorer{
		DBGetter:        rt.DBGetter(),
		DBDeleter:       rt.DBDeleter(),
		StageDir:        defaultStageDir(objectStoreRoot),
		ObjectStoreRoot: objectStoreRoot,
		DataDir:         filepath.Dir(objectStoreRoot),
		Logger:          logger,
		Progress: func(message string) {
			fmt.Fprintln(os.Stderr, message)
		},
	}

	if err := r.Restore(ctx, archivePath); err != nil {
		fmt.Fprintf(os.Stderr, "restore-backup: %v\n", err)
		// After the first database mutation there is no v1 rollback: the
		// controller must stay stopped and be rebootstraped/retried.
		if errors.Is(err, restore.ErrMutation) || errors.Is(err, restore.ErrFinalization) {
			fmt.Fprintln(os.Stderr,
				"restore: keep the controller stopped and rebootstrap/retry; no rollback is available")
		}
		return 1
	}

	fmt.Fprintln(os.Stderr, "restore-backup: restore complete; controller remains stopped")
	return 0
}

// defaultStageDir returns the private staging tree for archive blobs. It sits
// beside the object-store root so the final swap is a same-filesystem rename
// (a rename across filesystems would fail with EXDEV).
func defaultStageDir(objectStoreRoot string) string {
	return filepath.Join(filepath.Dir(objectStoreRoot), fmt.Sprintf("restore-stage-%d", os.Getpid()))
}

// defaultLogDir returns the log directory for the reduced-runtime database workers.
func defaultLogDir() string {
	dataRoot := os.Getenv("JUJU_LOG_DIR")
	if dataRoot == "" {
		dataRoot = "/var/log/juju"
	}
	return dataRoot
}

// defaultObjectStoreRoot returns the target file-object-store root under the
// Juju agent data directory. The standalone jujud packaging may rewrite this
// via environment.
func defaultObjectStoreRoot() string {
	dataRoot := os.Getenv("JUJU_DATA_DIR")
	if dataRoot == "" {
		dataRoot = "/var/lib/juju"
	}
	return filepath.Join(dataRoot, "objectstore")
}
