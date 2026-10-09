// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"path"
	"strings"

	"github.com/juju/errors"
	"github.com/juju/utils/v4"
	"github.com/juju/utils/v4/ssh"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
)

func uploadInitialisationFile(
	ctx environs.BootstrapContext,
	client ssh.Client,
	host string,
	params instancecfg.InitialisationFile,
	sshOptions *ssh.Options,
) error {
	remote := "ubuntu@" + host
	ctx.Infof("Uploading initialisation file to the controller")

	// The SSH clients hand the command arguments to the remote login
	// shell for re-parsing, so multi-word shell scripts cannot travel
	// as command arguments. The archive is transferred with scp into a
	// private (mktemp -d creates 0700) staging directory and installed
	// with single-word commands only: a fixed /tmp path would sit
	// world-readable until the install, and the archive holds the
	// database dumps and the CA private key.
	staging, err := client.Command(remote,
		[]string{"mktemp", "-d", "/tmp/juju-initialisation.XXXXXX"},
		sshOptions).Output()
	if err != nil {
		return errors.Annotate(err, "creating private staging directory on the controller")
	}
	stagingDir := strings.TrimSpace(string(staging))
	if path.Dir(stagingDir) != "/tmp" || !strings.HasPrefix(path.Base(stagingDir), "juju-initialisation.") || strings.ContainsAny(stagingDir, " \n\r\t") {
		return errors.New("invalid mktemp output from remote host")
	}

	if err := client.Copy([]string{
		params.Source, remote + ":" + stagingDir + "/archive",
	}, sshOptions); err != nil {
		return errors.Annotate(err, "uploading initialisation file to the controller")
	}

	verify := client.Command(remote, []string{"sha256sum", stagingDir + "/archive"}, sshOptions)
	out, err := verify.Output()
	if err != nil {
		return errors.Annotate(err, "verifying uploaded initialisation file")
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 || !strings.EqualFold(fields[0], params.SHA256) {
		return errors.New("uploaded initialisation file checksum mismatch")
	}
	install := client.Command(remote, []string{
		"sudo", "install", "-D", "-m", "0600",
		utils.ShQuote(stagingDir + "/archive"), utils.ShQuote(params.Destination + ".partial"),
	}, sshOptions)
	if out, err := install.CombinedOutput(); err != nil {
		return errors.Annotatef(err, "installing initialisation file on the controller: %s", strings.TrimSpace(string(out)))
	}

	if out, err := client.Command(remote, []string{"sudo", "mv", "-f", utils.ShQuote(params.Destination + ".partial"), utils.ShQuote(params.Destination)}, sshOptions).CombinedOutput(); err != nil {
		return errors.Annotatef(err, "publishing initialisation file: %s", out)
	}
	if _, err := client.Command(remote, []string{"rm", "-rf", stagingDir}, sshOptions).Output(); err != nil {
		// Non-fatal: the staging copy is private (0700) and the
		// machine is discarded when bootstrap fails.
		logger.Warningf(ctx, "removing recovery staging directory %q: %v", stagingDir, err)
	}
	return nil
}
