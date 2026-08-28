// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package restore

import (
	"testing"

	"github.com/juju/tc"

	"github.com/juju/juju/domain/export/types/controller/v4_1_0"
)

type compatibilitySuite struct{}

func TestCompatibilitySuite(t *testing.T) {
	tc.Run(t, &compatibilitySuite{})
}

func makeArchive(cloudName, cloudType string, regions []string, credName, credAuthType string) *Archive {
	credUUID := "cred-uuid"
	cloudUUID := "cloud-uuid"
	authTypeID := "0"
	return &Archive{
		ControllerUUID:      "source-ctrl",
		ControllerModelUUID: "source-ctrl-model",
		Controller: &v4_1_0.ControllerExport{
			Controller: []v4_1_0.Controller{{UUID: "source-ctrl", ModelUUID: "source-ctrl-model"}},
			Cloud:      []v4_1_0.Cloud{{UUID: cloudUUID, Name: cloudName, CloudTypeID: 1}},
			CloudType:  []v4_1_0.CloudType{{ID: new(int64(1)), Type: cloudType}},
			CloudCredential: []v4_1_0.CloudCredential{
				{UUID: credUUID, CloudUUID: cloudUUID, Name: credName, AuthTypeID: authTypeID, OwnerUUID: "owner"},
			},
			AuthType: []v4_1_0.AuthType{{ID: new(int64(0)), Type: new(credAuthType)}},
			Model: []v4_1_0.Model{{
				UUID:                "source-ctrl-model",
				CloudUUID:           cloudUUID,
				CloudCredentialUUID: &credUUID,
			}},
			CloudRegion: func() []v4_1_0.CloudRegion {
				var rs []v4_1_0.CloudRegion
				for _, r := range regions {
					rs = append(rs, v4_1_0.CloudRegion{CloudUUID: cloudUUID, Name: r})
				}
				return rs
			}(),
		},
	}
}

func makeTarget(cloudName, cloudType string, regions []string, credName, credAuthType string, modelCount, apps int) *Target {
	return &Target{
		ControllerUUID:      "target-ctrl",
		ControllerModelUUID: "target-ctrl-model",
		ControllerID:        "0",
		ModelNamespaces:     []string{"target-ctrl-model"},
		ModelCount:          modelCount,
		CloudName:           cloudName,
		CloudType:           cloudType,
		Regions:             regions,
		CredentialName:      credName,
		CredentialAuthType:  credAuthType,
		Applications:        apps,
	}
}

func (s *compatibilitySuite) TestCompatible(c *tc.C) {
	archive := makeArchive("lxd", "lxd", []string{"localhost"}, "default", "userpass")
	target := makeTarget("lxd", "lxd", []string{"localhost"}, "default", "userpass", 1, 0)
	c.Check(checkTargetCompatibility(archive, target), tc.ErrorIsNil)
}

func (s *compatibilitySuite) TestTargetWithExtraModelsRejected(c *tc.C) {
	archive := makeArchive("lxd", "lxd", []string{"localhost"}, "default", "userpass")
	target := makeTarget("lxd", "lxd", []string{"localhost"}, "default", "userpass", 2, 0)
	err := checkTargetCompatibility(archive, target)
	c.Assert(err, tc.ErrorIs, ErrTargetIncompatible)
}

func (s *compatibilitySuite) TestCloudNameMismatchRejected(c *tc.C) {
	archive := makeArchive("lxd", "lxd", []string{"localhost"}, "default", "userpass")
	target := makeTarget("other", "lxd", []string{"localhost"}, "default", "userpass", 1, 0)
	err := checkTargetCompatibility(archive, target)
	c.Assert(err, tc.ErrorIs, ErrTargetIncompatible)
}

func (s *compatibilitySuite) TestCloudTypeMismatchRejected(c *tc.C) {
	archive := makeArchive("aws", "ec2", []string{"us-east-1"}, "default", "access-key")
	target := makeTarget("aws", "gce", []string{"us-east-1"}, "default", "access-key", 1, 0)
	err := checkTargetCompatibility(archive, target)
	c.Assert(err, tc.ErrorIs, ErrTargetIncompatible)
}

func (s *compatibilitySuite) TestRegionMismatchRejected(c *tc.C) {
	archive := makeArchive("aws", "ec2", []string{"us-east-1", "us-west-2"}, "default", "access-key")
	target := makeTarget("aws", "ec2", []string{"us-east-1"}, "default", "access-key", 1, 0)
	err := checkTargetCompatibility(archive, target)
	c.Assert(err, tc.ErrorIs, ErrTargetIncompatible)
}

func (s *compatibilitySuite) TestCredentialMismatchRejected(c *tc.C) {
	archive := makeArchive("aws", "ec2", []string{"us-east-1"}, "default", "access-key")
	target := makeTarget("aws", "ec2", []string{"us-east-1"}, "other", "access-key", 1, 0)
	err := checkTargetCompatibility(archive, target)
	c.Assert(err, tc.ErrorIs, ErrTargetIncompatible)
}

func (s *compatibilitySuite) TestCredentialAuthTypeMismatchRejected(c *tc.C) {
	archive := makeArchive("aws", "ec2", []string{"us-east-1"}, "default", "access-key")
	target := makeTarget("aws", "ec2", []string{"us-east-1"}, "default", "userpass", 1, 0)
	err := checkTargetCompatibility(archive, target)
	c.Assert(err, tc.ErrorIs, ErrTargetIncompatible)
}
