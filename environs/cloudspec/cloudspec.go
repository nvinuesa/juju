// Copyright 2020 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloudspec

import (
	"net"
	"net/url"

	"github.com/juju/errors"
	"github.com/juju/names/v4"

	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/provider/lxd/lxdnames"
)

// CloudSpec describes a specific cloud configuration, for the purpose
// of opening an Environ to manage the cloud resources.
type CloudSpec struct {
	// Type is the type of cloud, eg aws, openstack etc.
	Type string

	// Name is the name of the cloud.
	Name string

	// Region is the name of the cloud region, if the cloud supports
	// regions.
	Region string

	// Endpoint is the endpoint for the cloud (region).
	Endpoint string

	// IdentityEndpoint is the identity endpoint for the cloud (region).
	IdentityEndpoint string

	// StorageEndpoint is the storage endpoint for the cloud (region).
	StorageEndpoint string

	// Credential is the cloud credential to use to authenticate
	// with the cloud, or nil if the cloud does not require any
	// credentials.
	Credential *jujucloud.Credential

	// CACertificates contains an optional list of Certificate
	// Authority certificates to be used to validate certificates
	// of cloud infrastructure components
	// The contents are Base64 encoded x.509 certs.
	CACertificates []string

	// SkipTLSVerify is true if the client should be asked not to
	// validate certificates. It is not recommended for production clouds.
	// It is secure (false) by default.
	SkipTLSVerify bool

	// IsControllerCloud is true when this is the cloud used by the controller.
	IsControllerCloud bool
}

// Validate validates that the CloudSpec is well-formed. It does
// not ensure that the cloud type and credentials are valid.
func (cs CloudSpec) Validate() error {
	if cs.Type == "" {
		return errors.NotValidf("empty Type")
	}
	if !names.IsValidCloud(cs.Name) {
		return errors.NotValidf("cloud name %q", cs.Name)
	}
	return nil
}

// IsLocalhostCloud returns true if the cloud is an LXD localhost cloud.
func (cs *CloudSpec) IsLocalhostCloud() (bool, error) {
	if cs.Type != lxdnames.ProviderType &&
		cs.Type != lxdnames.DefaultCloud {
		return false, nil
	}
	if cs.Region != lxdnames.DefaultLocalRegion {
		return false, nil
	}
	endpointURL, err := url.Parse(cs.Endpoint)
	if err != nil {
		return false, errors.Trace(err)
	}
	endpointIP := net.ParseIP(endpointURL.Hostname())
	if endpointIP == nil {
		return false, nil
	}
	return network.IsLocalAddress(endpointIP)
}

// MakeCloudSpec returns a CloudSpec from the given
// Cloud, cloud and region names, and credential.
func MakeCloudSpec(cloud jujucloud.Cloud, cloudRegionName string, credential *jujucloud.Credential) (CloudSpec, error) {
	cloudSpec := CloudSpec{
		Type:              cloud.Type,
		Name:              cloud.Name,
		Region:            cloudRegionName,
		Endpoint:          cloud.Endpoint,
		IdentityEndpoint:  cloud.IdentityEndpoint,
		StorageEndpoint:   cloud.StorageEndpoint,
		CACertificates:    cloud.CACertificates,
		SkipTLSVerify:     cloud.SkipTLSVerify,
		Credential:        credential,
		IsControllerCloud: cloud.IsControllerCloud,
	}
	if cloudRegionName != "" {
		cloudRegion, err := jujucloud.RegionByName(cloud.Regions, cloudRegionName)
		if err != nil {
			return CloudSpec{}, errors.Annotate(err, "getting cloud region definition")
		}
		if !cloudRegion.IsEmpty() {
			cloudSpec.Endpoint = cloudRegion.Endpoint
			cloudSpec.IdentityEndpoint = cloudRegion.IdentityEndpoint
			cloudSpec.StorageEndpoint = cloudRegion.StorageEndpoint
		}
	}
	return cloudSpec, nil
}

// CloudRegionSpec contains the information needed to lookup specific
// cloud or cloud region configuration. This is for use in calling
// state/modelconfig.(ComposeNewModelConfig) so there is no need to serialize it.
type CloudRegionSpec struct {
	// Cloud is the name of the cloud.
	Cloud string

	// Region is the name of the cloud region.
	Region string
}

// NewCloudRegionSpec returns a CloudRegionSpec ensuring cloud arg is not empty.
func NewCloudRegionSpec(cloud, region string) (*CloudRegionSpec, error) {
	if cloud == "" {
		return nil, errors.New("cloud is required to be non empty")
	}
	return &CloudRegionSpec{Cloud: cloud, Region: region}, nil
}
