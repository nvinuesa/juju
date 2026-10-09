// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/juju/utils/v4"

	"github.com/juju/juju/internal/errors"
)

func (c *controllerStack) ensureControllerInitialisation(ctx context.Context) error {
	cfg := c.pcfg.Initialisation
	if cfg == nil {
		return c.ensureControllerConfigmapBootstrapParams(ctx)
	}
	if cfg.SetupCommand == "" || cfg.Directory == "" || path.IsAbs(cfg.Directory) || strings.Contains(cfg.Directory, "..") || len(cfg.Files) == 0 {
		return errors.New("incomplete controller initialisation configuration")
	}
	cm, err := c.getControllerConfigMap(ctx)
	if err != nil {
		return errors.Capture(err)
	}
	for filename, contents := range cfg.Files {
		if filename == "" || path.Base(filename) != filename || filename == "." || filename == ".." {
			return errors.New("invalid controller initialisation filename")
		}
		cm.Data[filename] = contents
	}
	cleanup, err := c.broker.ensureConfigMap(ctx, cm)
	c.addCleanUp(cleanup)
	return errors.Capture(err)
}

func (c *controllerStack) initialisationSeedCommand() string {
	cfg := c.pcfg.Initialisation
	files := make([]string, 0, len(cfg.Files))
	for filename := range cfg.Files {
		files = append(files, filename)
	}
	sort.Strings(files)
	destination := path.Join(c.pcfg.DataDir, cfg.Directory)
	commands := []string{`if [ "${controller_id}" = "0" ]; then`, "mkdir -p " + utils.ShQuote(destination)}
	for _, filename := range files {
		target := utils.ShQuote(path.Join(destination, filename))
		commands = append(commands, "cp "+utils.ShQuote(path.Join(controllerConfigSeedDir, filename))+" "+target, "chmod 600 "+target)
	}
	return strings.Join(append(commands, "fi"), "\n") + "\n"
}

func controllerMachineCommand() string {
	return fmt.Sprintf(`/bin/sh -c 'controller_id="${HOSTNAME##*-}"; exec %s machine --data-dir "$JUJU_DATA_DIR" --controller-id "${controller_id}" --log-to-stderr --show-log'`, "$JUJU_TOOLS_DIR/jujuagentd")
}
