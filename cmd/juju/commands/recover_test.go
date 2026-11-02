// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"testing"

	"github.com/juju/errors"
	"github.com/juju/tc"

	"github.com/juju/juju/api/jujuclient"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/cmd/cmdtesting"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/domain/recovery"
	"github.com/juju/juju/environs"
)

type recoverySuite struct{}

func TestRecoverySuite(t *testing.T) { tc.Run(t, &recoverySuite{}) }

func (s *recoverySuite) TestInit(c *tc.C) {
	for _, test := range []struct {
		args    []string
		pattern string
	}{
		{nil, "a backup archive is required"},
		{[]string{"backup.tar.gz"}, "a backup assertion file is required"},
		{[]string{"", "backup.tar.gz.assert"}, "a backup archive is required"},
		{[]string{"backup.tar.gz", ""}, "a backup assertion file is required"},
		{[]string{"backup.tar.gz", "backup.tar.gz.assert", "extra"}, "unrecognized args.*"},
		{[]string{"backup.tar.gz", "backup.tar.gz.assert", "--sha256", "bad"}, "(flag|option) provided but not defined.*"},
		{[]string{"backup.tar.gz", "backup.tar.gz.assert", "--credential", "source"}, "(flag|option) provided but not defined.*"},
		{[]string{"backup.tar.gz", "backup.tar.gz.assert", "--config", "ca-cert=override"}, "(flag|option) provided but not defined.*"},
	} {
		err := cmdtesting.InitCommand(newRecoveryCommand(), test.args)
		c.Check(err, tc.ErrorMatches, test.pattern)
	}
	// Both paths are recorded for recovery, as given.
	command := &recoveryCommand{}
	wrapped := modelcmd.Wrap(command, modelcmd.WrapSkipModelFlags, modelcmd.WrapSkipDefaultModel)
	err := cmdtesting.InitCommand(wrapped, []string{"backup.tar.gz", "elsewhere/backup.tar.gz.assert"})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(command.archivePath, tc.Equals, "backup.tar.gz")
	c.Check(command.assertionPath, tc.Equals, "elsewhere/backup.tar.gz.assert")
}

func (s *recoverySuite) TestCredentialSelection(c *tc.C) {
	provider, err := environs.Provider("dummy")
	c.Assert(err, tc.ErrorIsNil)
	provider = noDetectionProvider{provider}
	for _, mode := range []string{"local", "archived", "ambiguous", "revoked", "missing"} {
		store := jujuclient.NewMemStore()
		info := &recovery.ArchiveInfo{Region: "source-region", Credential: &recovery.CredentialInfo{
			Name: "archived", AuthType: string(cloud.UserPassAuthType), Attributes: map[string]string{"username": "source", "password": "secret"},
		}}
		if mode == "local" || mode == "ambiguous" {
			credentials := map[string]cloud.Credential{"local": cloud.NewCredential(cloud.UserPassAuthType, map[string]string{"username": "local", "password": "fresh"})}
			if mode == "ambiguous" {
				credentials["second"] = credentials["local"]
			}
			store.Credentials["dummy"] = cloud.CloudCredential{AuthCredentials: credentials}
		}
		if mode == "revoked" {
			info.Credential.Revoked = true
		}
		if mode == "missing" {
			info.Credential = nil
		}
		p := recoveryCommand{recoveryInfo: info}
		p.SetClientStore(store)
		credentials, region, err := p.recoveryCredentials(cmdtesting.Context(c), provider, cloud.Cloud{
			Name: "dummy", Type: "dummy", AuthTypes: []cloud.AuthType{cloud.UserPassAuthType},
			Regions: []cloud.Region{{Name: "source-region"}},
		})
		switch mode {
		case "ambiguous":
			c.Check(err, tc.ErrorIs, modelcmd.ErrMultipleCredentials)
		case "revoked":
			c.Check(err, tc.ErrorMatches, "archived controller credential is revoked or invalid.*")
		case "missing":
			c.Check(err, tc.ErrorMatches, "no local or archived controller credential is available.*")
		default:
			c.Assert(err, tc.ErrorIsNil)
			c.Check(credentials.name, tc.Equals, mode)
			c.Check(region, tc.Equals, "source-region")
		}
	}
}

type failingRecoveryProvider struct{ environs.EnvironProvider }

func (p failingRecoveryProvider) FinalizeCredential(environs.FinalizeCredentialContext, environs.FinalizeCredentialParams) (*cloud.Credential, error) {
	return nil, errors.New("credential authentication failed")
}

func (s *recoverySuite) TestLocalCredentialErrorDoesNotFallBack(c *tc.C) {
	provider, err := environs.Provider("dummy")
	c.Assert(err, tc.ErrorIsNil)
	store := jujuclient.NewMemStore()
	// A selected local credential that fails authentication must not fall back.
	store.Credentials["dummy"] = cloud.CloudCredential{AuthCredentials: map[string]cloud.Credential{
		"local": cloud.NewCredential(cloud.UserPassAuthType, map[string]string{"username": "local", "password": "fresh"}),
	}}
	p := recoveryCommand{recoveryInfo: &recovery.ArchiveInfo{Credential: &recovery.CredentialInfo{Name: "archived"}}}
	p.SetClientStore(store)
	_, _, err = p.recoveryCredentials(cmdtesting.Context(c), failingRecoveryProvider{provider}, cloud.Cloud{
		Name: "dummy", Type: "dummy", AuthTypes: []cloud.AuthType{cloud.UserPassAuthType},
	})
	c.Check(err, tc.ErrorMatches, ".*credential authentication failed.*")
}

type noDetectionProvider struct{ environs.EnvironProvider }

func (p noDetectionProvider) DetectCredentials(string) (*cloud.CloudCredential, error) {
	return nil, errors.NotFoundf("local credentials")
}
