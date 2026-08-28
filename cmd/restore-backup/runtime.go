// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package main

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/juju/clock"
	"github.com/juju/names/v6"
	"github.com/juju/utils/v4/voyeur"
	"github.com/juju/worker/v5"
	"github.com/juju/worker/v5/dependency"
	"gopkg.in/tomb.v2"

	"github.com/juju/juju/agent"
	agentengine "github.com/juju/juju/agent/engine"
	cmdsafemode "github.com/juju/juju/cmd/jujuagentd/agent/safemode"
	coredatabase "github.com/juju/juju/core/database"
	corelogger "github.com/juju/juju/core/logger"
	"github.com/juju/juju/internal/errors"
	internallogger "github.com/juju/juju/internal/logger"
	"github.com/juju/juju/internal/restore"
	"github.com/juju/juju/internal/worker/dbaccessor"
	"github.com/juju/juju/internal/worker/gate"
	jujunames "github.com/juju/juju/juju/names"
)

const accessorDeliverTimeout = 60 * time.Second

// accessorDelivery carries the db-accessor worker's DB access interfaces
// extracted once the safe-mode engine has brought it up.
type accessorDelivery struct {
	Getter  coredatabase.DBGetter
	Deleter coredatabase.DBDeleter
}

// runtimeImpl is the reduced runtime: the same safe-mode engine as
// jujuagentd --safe-mode runs, with an accessor manifold delivering the
// db-accessor's DBGetter/DBDeleter to this command.
type runtimeImpl struct {
	engine  *dependency.Engine
	getter  coredatabase.DBGetter
	deleter coredatabase.DBDeleter
}

func (r *runtimeImpl) DBGetter() restore.DBGetter   { return r.getter }
func (r *runtimeImpl) DBDeleter() restore.DBDeleter { return r.deleter }
func (r *runtimeImpl) Close() error {
	r.engine.Kill()
	return r.engine.Wait()
}

// idleWorker is a no-op worker whose sole job is to let the accessor
// manifold sit in the engine after it delivered the db-accessor interfaces.
type idleWorker struct {
	t tomb.Tomb
}

func (w *idleWorker) Kill()       { w.t.Kill(nil) }
func (w *idleWorker) Wait() error { return w.t.Wait() }

// accessorManifold is a dependency manifold that waits for the db-accessor
// worker and ships its DB access interfaces over deliver.
func accessorManifold(deliver chan accessorDelivery) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{"db-accessor"},
		Start: func(ctx context.Context, getter dependency.Getter) (worker.Worker, error) {
			var dbGetter coredatabase.DBGetter
			if err := getter.Get("db-accessor", &dbGetter); err != nil {
				return nil, errors.Errorf("getting db-accessor as DBGetter: %w", err)
			}
			var dbDeleter coredatabase.DBDeleter
			if err := getter.Get("db-accessor", &dbDeleter); err != nil {
				return nil, errors.Errorf("getting db-accessor as DBDeleter: %w", err)
			}
			select {
			case deliver <- accessorDelivery{Getter: dbGetter, Deleter: dbDeleter}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &idleWorker{}, nil
		},
	}
}

// machineAgent adapts the on-disk machine agent config to the [agent.Agent]
// interface the agent manifold consumes.
type machineAgent struct {
	conf agent.ConfigSetterWriter
	tag  names.Tag
}

func (a *machineAgent) CurrentConfig() agent.Config { return a.conf }

func (a *machineAgent) ChangeConfig(mutator agent.ConfigMutator) error {
	if err := mutator(a.conf); err != nil {
		return err
	}
	return a.conf.Write()
}

// staticStartupValues provides controller startup values read once from the
// machine agent config. Unlike the machine's provider it does not re-read
// the file: the restore command runs to completion and never rebounces.
type staticStartupValues struct {
	values dbaccessor.ControllerStartupValues
}

func (p staticStartupValues) ControllerStartupValues() (dbaccessor.ControllerStartupValues, error) {
	return p.values, nil
}

// startupValuesFromConfig builds controller startup values the same way
// machineControllerStartupValueProvider does, without requiring a live agent.
func startupValuesFromConfig(conf agent.Config) (dbaccessor.ControllerStartupValues, error) {
	info, _ := conf.ControllerAgentInfo()
	dqlitePort, _ := conf.DqlitePort()
	return dbaccessor.ControllerStartupValues{
		ControllerID:          conf.Tag().Id(),
		DataDir:               conf.DataDir(),
		DqlitePort:            dqlitePort,
		QueryTracingEnabled:   conf.QueryTracingEnabled(),
		QueryTracingThreshold: conf.QueryTracingThreshold(),
		DqliteBusyTimeout:     conf.DqliteBusyTimeout(),
		CACert:                conf.CACert(),
		ControllerCert:        info.Cert,
		ControllerPrivateKey:  info.PrivateKey,
	}, nil
}

// newRuntime starts the reduced safe-mode runtime and returns the DB access
// it provides. On Close it shuts the engine down.
func newRuntime(dataDir string) (runtime, error) {
	if !dqliteEnabled {
		return nil, errors.New(
			"restore-backup was built without Dqlite support; rebuild it with `make restore-backup`",
		)
	}

	ctx := context.Background()
	tag := names.NewMachineTag("0")
	logger := internallogger.GetLogger("juju.restore")

	// Live-target refusal. On IAAS the machine agent service must be down;
	// on k8s hold there is no systemctl and we rely on exclusive Dqlite
	// access instead.
	if _, err := exec.LookPath("systemctl"); err == nil {
		if err := ensureJujudNotRunning(tag); err != nil {
			return nil, errors.Errorf("machine agent is still running: %w", err)
		}
	} else {
		logger.Warningf(ctx, "systemctl not found; skipping live-target check (K8s hold)")
	}

	// Read the on-disk agent config.
	confPath := agent.ConfigPath(dataDir, tag)
	conf, err := agent.ReadConfig(confPath)
	if err != nil {
		return nil, errors.Errorf("reading agent config %q: %w", confPath, err)
	}

	values, err := startupValuesFromConfig(conf)
	if err != nil {
		return nil, errors.Errorf("building controller startup values: %w", err)
	}

	eng, err := dependency.NewEngine(agentengine.DependencyEngineConfig(
		dependency.DefaultMetrics(),
		dependencyLogger{logger: internallogger.GetLogger("juju.dependency")},
	))
	if err != nil {
		return nil, errors.Errorf("creating dependency engine: %w", err)
	}

	deliver := make(chan accessorDelivery, 1)
	cfg := cmdsafemode.ManifoldsConfig{
		Agent:                   &machineAgent{conf: conf, tag: tag},
		AgentConfigChanged:      voyeur.NewValue(true),
		NewDBWorkerFunc:         dbaccessor.NewTrackedDBWorker,
		ControllerStartupValues: staticStartupValues{values: values},
		ControllerUnlocker:      gate.NewLock(),
		ControllerID:            tag.Id(),
		LogDir:                  defaultLogDir(),
		ConfigChangeSocketPath:  path.Join(conf.DataDir(), "configchange.socket"),
		Clock:                   clock.WallClock,
	}
	manifolds := cmdsafemode.Manifolds(cfg)
	manifolds["restore-accessor"] = accessorManifold(deliver)

	if err := dependency.Install(eng, manifolds); err != nil {
		eng.Kill()
		_ = eng.Wait()
		return nil, errors.Errorf("installing manifolds: %w", err)
	}

	var delivery accessorDelivery
	select {
	case delivery = <-deliver:
	case <-time.After(accessorDeliverTimeout):
		eng.Kill()
		_ = eng.Wait()
		return nil, errors.Errorf("timed out waiting for db accessor reduced runtime")
	}

	return &runtimeImpl{engine: eng, getter: delivery.Getter, deleter: delivery.Deleter}, nil
}

// ensureJujudNotRunning uses systemctl to refuse a live target. A missing
// unit or a non-active status means the controller service is stopped.
func ensureJujudNotRunning(tag names.Tag) error {
	unit := fmt.Sprintf("%s-machine-%s.service", jujunames.JujuAgentd, tag.Id())
	cmd := exec.Command("systemctl", "check", unit)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// systemctl exits non-zero when the unit is not "active" (e.g.
		// inactive/failed/missing). Anything other than "active" output
		// means the agent is not running; there is nothing to refuse.
		if _, isExit := err.(*exec.ExitError); isExit {
			return nil
		}
		return errors.Errorf("cannot check jujud status: %w", err)
	}
	if strings.TrimSpace(string(output)) != "active" {
		return nil
	}
	return errors.Errorf("jujud is running")
}

// dependencyLogger forwards engine log lines to stderr for the restore
// command. The phase logs stay readable; engine diagnostics help debug
// the reduced runtime (e.g. accessor start failures).
type dependencyLogger struct {
	logger corelogger.Logger
}

func (d dependencyLogger) Tracef(format string, args ...any) {
	d.logger.Tracef(context.Background(), format, args...)
}
func (d dependencyLogger) Debugf(format string, args ...any) {
	d.logger.Debugf(context.Background(), format, args...)
}
func (d dependencyLogger) Infof(format string, args ...any) {
	d.logger.Infof(context.Background(), format, args...)
}
func (d dependencyLogger) Warnf(format string, args ...any) {
	d.logger.Errorf(context.Background(), format, args...)
}
func (d dependencyLogger) Errorf(format string, args ...any) {
	d.logger.Errorf(context.Background(), format, args...)
}
func (d dependencyLogger) Criticalf(format string, args ...any) {
	d.logger.Errorf(context.Background(), format, args...)
}
