// Copyright 2018 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package provider_test

import (
	"context"

	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/caas"
	"github.com/juju/juju/caas/kubernetes/provider"
	k8stesting "github.com/juju/juju/caas/kubernetes/provider/testing"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/environs"
	environscloudspec "github.com/juju/juju/environs/cloudspec"
	"github.com/juju/juju/environs/config"
	coretesting "github.com/juju/juju/internal/testing"
	"github.com/juju/juju/internal/uuid"
)

func fakeConfig(c *gc.C, attrs ...coretesting.Attrs) *config.Config {
	cfg, err := coretesting.ModelConfig(c).Apply(fakeConfigAttrs(attrs...))
	c.Assert(err, jc.ErrorIsNil)
	return cfg
}

func fakeConfigAttrs(attrs ...coretesting.Attrs) coretesting.Attrs {
	merged := coretesting.FakeConfig().Merge(coretesting.Attrs{
		"type":             "kubernetes",
		"uuid":             uuid.MustNewUUID().String(),
		"workload-storage": "",
	})
	for _, attrs := range attrs {
		merged = merged.Merge(attrs)
	}
	return merged
}

func fakeCloudSpec() environscloudspec.CloudSpec {
	cred := fakeCredential()
	return environscloudspec.CloudSpec{
		Type:       "kubernetes",
		Name:       "k8s",
		Endpoint:   "host1",
		Credential: &cred,
	}
}

func fakeCredential() cloud.Credential {
	return cloud.NewCredential(cloud.UserPassAuthType, map[string]string{
		"username": "user1",
		"password": "password1",
	})
}

type providerSuite struct {
	testing.IsolationSuite
	dialStub testing.Stub
	provider caas.ContainerEnvironProvider
}

var _ = gc.Suite(&providerSuite{})

func (s *providerSuite) SetUpTest(c *gc.C) {
	s.IsolationSuite.SetUpTest(c)
	s.dialStub.ResetCalls()
	s.provider = provider.NewProvider()
}

func (s *providerSuite) TestRegistered(c *gc.C) {
	provider, err := environs.Provider("kubernetes")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(provider, gc.NotNil)
}

func (s *providerSuite) TestOpen(c *gc.C) {
	s.PatchValue(&provider.NewK8sClients, k8stesting.NoopFakeK8sClients)
	config := fakeConfig(c)
	broker, err := s.provider.Open(context.Background(), environs.OpenParams{
		Cloud:  fakeCloudSpec(),
		Config: config,
	}, environs.NoopCredentialInvalidator())
	c.Check(err, jc.ErrorIsNil)
	c.Assert(broker, gc.NotNil)
}

func (s *providerSuite) TestOpenInvalidCloudSpec(c *gc.C) {
	spec := fakeCloudSpec()
	spec.Name = ""
	s.testOpenError(c, spec, `validating cloud spec: cloud name "" not valid`)
}

func (s *providerSuite) TestOpenMissingCredential(c *gc.C) {
	spec := fakeCloudSpec()
	spec.Credential = nil
	s.testOpenError(c, spec, `validating cloud spec: missing credential not valid`)
}

func (s *providerSuite) TestOpenUnsupportedCredential(c *gc.C) {
	credential := cloud.NewCredential(cloud.OAuth1AuthType, map[string]string{})
	spec := fakeCloudSpec()
	spec.Credential = &credential
	s.testOpenError(c, spec, `validating cloud spec: "oauth1" auth-type not supported`)
}

func (s *providerSuite) testOpenError(c *gc.C, spec environscloudspec.CloudSpec, expect string) {
	_, err := s.provider.Open(context.Background(), environs.OpenParams{
		Cloud:  spec,
		Config: fakeConfig(c),
	}, environs.NoopCredentialInvalidator())
	c.Assert(err, gc.ErrorMatches, expect)
}

func (s *providerSuite) TestValidateCloud(c *gc.C) {
	err := s.provider.ValidateCloud(context.Background(), fakeCloudSpec())
	c.Check(err, jc.ErrorIsNil)
}

func (s *providerSuite) TestValidate(c *gc.C) {
	config := fakeConfig(c)
	validCfg, err := s.provider.Validate(context.Background(), config, nil)
	c.Check(err, jc.ErrorIsNil)

	validAttrs := validCfg.AllAttrs()
	c.Assert(config.AllAttrs(), gc.DeepEquals, validAttrs)
}
