// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloudspec_test

import (
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	environscloudspec "github.com/juju/juju/environs/cloudspec"
	"github.com/juju/juju/provider/lxd/lxdnames"
)

type cloudSpecSuite struct {
}

var _ = gc.Suite(&cloudSpecSuite{})

func (s *cloudSpecSuite) TestNewRegionSpec(c *gc.C) {
	tests := []struct {
		description, cloud, region, errMatch string
		nilErr                               bool
		want                                 *environscloudspec.CloudRegionSpec
	}{
		{
			description: "test empty cloud",
			cloud:       "",
			region:      "aregion",
			errMatch:    "cloud is required to be non empty",
			want:        nil,
		}, {
			description: "test empty region",
			cloud:       "acloud",
			region:      "",
			nilErr:      true,
			want:        &environscloudspec.CloudRegionSpec{Cloud: "acloud"},
		}, {
			description: "test valid",
			cloud:       "acloud",
			region:      "aregion",
			nilErr:      true,
			want:        &environscloudspec.CloudRegionSpec{Cloud: "acloud", Region: "aregion"},
		},
	}
	for i, test := range tests {
		c.Logf("Test %d: %s", i, test.description)
		rspec, err := environscloudspec.NewCloudRegionSpec(test.cloud, test.region)
		if !test.nilErr {
			c.Check(err, gc.ErrorMatches, test.errMatch)
		} else {
			c.Check(err, jc.ErrorIsNil)
		}
		c.Check(rspec, jc.DeepEquals, test.want)
	}
}

func (s *cloudSpecSuite) TestIsLocalhostCloud(c *gc.C) {
	tests := []struct {
		description string
		cloudType   string
		region      string
		endpoint    string
		expected    bool
		expectedErr string
	}{
		{
			description: "not lxd nor localhost cloud type",
			cloudType:   "aws",
			expected:    false,
		},
		{
			description: "not default region name",
			cloudType:   lxdnames.ProviderType,
			region:      "us-east-1",
			expected:    false,
		},
		{
			description: "wrongly formatted endpoint",
			cloudType:   lxdnames.ProviderType,
			region:      lxdnames.DefaultLocalRegion,
			endpoint:    "::",
			expected:    false,
			expectedErr: ".*missing protocol scheme.*",
		},
		{
			description: "endpoint does not contain IP",
			cloudType:   lxdnames.ProviderType,
			region:      lxdnames.DefaultLocalRegion,
			endpoint:    "localhost:5432",
			expected:    false,
		},
		{
			description: "endpoint is remote IP",
			cloudType:   lxdnames.ProviderType,
			region:      lxdnames.DefaultLocalRegion,
			endpoint:    "http://8.8.8.8:5432",
			expected:    false,
		},
		{
			description: "endpoint is local IP with providerType cloudType",
			cloudType:   lxdnames.ProviderType,
			region:      lxdnames.DefaultLocalRegion,
			endpoint:    "http://127.0.0.1:5432",
			expected:    true,
		},
		{
			description: "endpoint is local IP with defaultCloud cloudType",
			cloudType:   lxdnames.DefaultCloud,
			region:      lxdnames.DefaultLocalRegion,
			endpoint:    "http://127.0.0.1:5432",
			expected:    true,
		},
	}
	for i, tt := range tests {
		c.Logf("Test %d: %s", i, tt.description)
		cloud := environscloudspec.CloudSpec{
			Type:     tt.cloudType,
			Region:   tt.region,
			Endpoint: tt.endpoint,
		}
		isLocal, err := cloud.IsLocalhostCloud()
		if err != nil {
			c.Check(err, gc.ErrorMatches, tt.expectedErr)
			// when err is returned, isLocalhostCloud is false
			c.Check(isLocal, gc.Equals, false)
			continue
		}
		c.Check(err, jc.ErrorIsNil)
		c.Check("", gc.Equals, tt.expectedErr)

		c.Check(isLocal, gc.Equals, tt.expected)
	}
}
