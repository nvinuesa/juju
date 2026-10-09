// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"fmt"
	"strings"

	"github.com/juju/tc"

	"github.com/juju/juju/internal/recovery"
)

func (s *validateSuite) TestK8sControllerPlatformComesFromDump(c *tc.C) {
	for _, architecture := range []string{"amd64", "arm64"} {
		files := k8sPlatformFiles(architecture)
		filename, sum := writeArchive(c, files)
		info, err := recovery.ValidateArchive(c.Context(), filename, sum)
		c.Assert(err, tc.ErrorIsNil)
		c.Check(info.ControllerArchitecture, tc.Equals, architecture)
		c.Check(info.ControllerCharmBase, tc.Equals, "ubuntu@24.04/stable")
	}
}

func (s *validateSuite) TestK8sMissingPlatformDoesNotUseClientDefault(c *tc.C) {
	files := k8sPlatformFiles("amd64")
	files["juju-backup/dump/models/"+testControllerModelUUID+".yaml"] = []byte("payload: {}\n")
	filename, sum := writeArchive(c, files)
	_, err := recovery.ValidateArchive(c.Context(), filename, sum)
	c.Check(err, tc.ErrorMatches, "archive must identify one Kubernetes controller application")
	files = k8sPlatformFiles("unknown")
	filename, sum = writeArchive(c, files)
	_, err = recovery.ValidateArchive(c.Context(), filename, sum)
	c.Check(err, tc.ErrorMatches, "archive does not record a supported Kubernetes controller architecture")
}

func k8sPlatformFiles(architecture string) map[string][]byte {
	files := dumpOnlyFiles(strings.ReplaceAll(controllerDump(modelRow(testControllerModelUUID, "controller", "cloud-k8s")), "model_type_id: 0", "model_type_id: 1"))
	files["juju-backup/dump/models/"+testControllerModelUUID+".yaml"] = []byte(fmt.Sprintf(`version: 4.1.0
payload:
  application_controller:
  - application_uuid: archived-controller-app
  application_platform:
  - application_uuid: other-app
    architecture_id: 0
    os_id: '0'
    channel: 22.04/stable
  - application_uuid: archived-controller-app
    architecture_id: 1
    os_id: '0'
    channel: 24.04/stable
  architecture:
  - id: 0
    name: ignored
  - id: 1
    name: %s
  os:
  - id: 0
    name: ubuntu
`, architecture))
	return files
}
