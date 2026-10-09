//go:build !dqlite

// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package recovery_test

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/juju/names/v6"
	"github.com/juju/tc"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/agent/agentrecovery"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/core/database"
	"github.com/juju/juju/core/model"
	"github.com/juju/juju/core/network"
	"github.com/juju/juju/core/version"
	"github.com/juju/juju/internal/auth"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	loggertesting "github.com/juju/juju/internal/logger/testing"
	"github.com/juju/juju/internal/recovery"
	coretesting "github.com/juju/juju/internal/testing"
)

type initialiseSuite struct{ coretesting.BaseSuite }

func TestRecoveryInitialiseSuite(t *testing.T) { tc.Run(t, &initialiseSuite{}) }

func (s *initialiseSuite) TestImportAndPasswords(c *tc.C) {
	files := validFiles()
	files["juju-backup/metadata.json"] = []byte(metadataJSON(version.Current.String()))
	controllerDump := string(files["juju-backup/dump/controller.yaml"])
	controllerDump = strings.ReplaceAll(controllerDump, "ca_cert: source-ca-cert", "ca_cert: "+strconv.Quote(coretesting.CACert))
	controllerDump = strings.ReplaceAll(controllerDump, "ca_private_key: source-ca-key", "ca_private_key: "+strconv.Quote(coretesting.CAKey))
	controllerDump += `  user:
  - uuid: admin-uuid
    name: admin
    external: false
    removed: false
    created_by_uuid: admin-uuid
    created_at: 2026-01-01T00:00:00Z
  - uuid: other-uuid
    name: other
    external: false
    removed: false
    created_by_uuid: admin-uuid
    created_at: 2026-01-01T00:00:00Z
  user_authentication:
  - user_uuid: admin-uuid
    disabled: false
  - user_uuid: other-uuid
    disabled: false
  user_password:
  - user_uuid: other-uuid
    password_hash: unchanged-hash
    password_salt: unchanged-salt
`
	controllerDump = strings.Replace(controllerDump, "name: controller\n", "name: controller\n    cloud_credential_uuid: source-credential\n", 1)
	controllerDump += `  auth_type:
  - id: 2
    type: userpass
  cloud_credential:
  - uuid: source-credential
    cloud_uuid: cloud-lxd
    auth_type_id: '2'
    owner_uuid: admin-uuid
    name: original
    invalid: true
    revoked: false
  cloud_credential_attribute:
  - cloud_credential_uuid: source-credential
    key: password
    value: stale
`
	files["juju-backup/dump/controller.yaml"] = []byte(controllerDump)
	files["juju-backup/dump/models/"+testControllerModelUUID+".yaml"] = []byte(controllerModelDumpYAML)
	files["juju-backup/dump/models/"+testModelAUUID+".yaml"] = []byte(emptyModelDumpYAML)
	files["juju-backup/dump/models/"+testModelBUUID+".yaml"] = []byte(emptyModelDumpYAML)
	files["juju-backup/root.tar"] = rootTar(c)
	archive, sum := writeArchive(c, files)
	dir := c.MkDir()
	cfg, err := agent.NewStateMachineConfig(agent.AgentConfigParams{
		Paths: agent.Paths{DataDir: dir}, Tag: names.NewMachineTag("0"), Jobs: []model.MachineJob{model.JobManageModel},
		UpgradedToVersion: version.Current, Password: "client-secret", Controller: names.NewControllerTag(testControllerUUID),
		Model: names.NewModelTag(testControllerModelUUID), APIAddresses: []string{"127.0.0.1:17070"}, CACert: coretesting.CACert,
	}, controller.ControllerAgentInfo{APIPort: 17070, Cert: coretesting.ServerCert, PrivateKey: coretesting.ServerKey, CAPrivateKey: coretesting.CAKey})
	c.Assert(err, tc.ErrorIsNil)
	addresses := network.NewMachineAddresses([]string{"10.0.0.2"}, network.WithScope(network.ScopeCloudLocal)).AsProviderAddresses()
	credential := cloud.NewCredential(cloud.UserPassAuthType, map[string]string{"username": "local", "password": "fresh"})
	params := recovery.Params{ArchivePath: archive, SHA256: sum, AgentVersion: version.Current, MachineName: "0", MachineNonce: "target-nonce",
		Node: instancecfg.StateInitializationParams{ControllerCloudCredential: &credential, BootstrapMachineInstanceId: "replacement-instance", BootstrapMachineDisplayName: "replacement-display"}}
	c.Assert(agentrecovery.Initialise(c.Context(), cfg, params, addresses, loggertesting.WrapCheckLog(c)), tc.ErrorIsNil)
	apiInfo, ok := cfg.APIInfo()
	c.Assert(ok, tc.IsTrue)
	c.Check(apiInfo.Password, tc.Not(tc.Equals), "client-secret")
	db, err := sql.Open("sqlite3", filepath.Join(dir, "dqlite", database.ControllerNS))
	c.Assert(err, tc.ErrorIsNil)
	defer db.Close()
	var hash string
	var salt []byte
	c.Assert(db.QueryRow("SELECT password_hash, password_salt FROM user_password WHERE user_uuid = 'admin-uuid'").Scan(&hash, &salt), tc.ErrorIsNil)
	password := auth.NewPassword("client-secret")
	defer password.Destroy()
	want, err := auth.HashPassword(password, salt)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(hash, tc.Equals, want)
	c.Assert(db.QueryRow("SELECT password_hash FROM user_password WHERE user_uuid = 'other-uuid'").Scan(&hash), tc.ErrorIsNil)
	c.Check(hash, tc.Equals, "unchanged-hash")
	var credentialPassword string
	var invalid bool
	c.Assert(db.QueryRow("SELECT value FROM cloud_credential_attribute WHERE cloud_credential_uuid = 'source-credential' AND key = 'password'").Scan(&credentialPassword), tc.ErrorIsNil)
	c.Check(credentialPassword, tc.Equals, "fresh")
	c.Assert(db.QueryRow("SELECT invalid FROM cloud_credential WHERE uuid = 'source-credential'").Scan(&invalid), tc.ErrorIsNil)
	c.Check(invalid, tc.IsFalse)
	modelDB, err := sql.Open("sqlite3", filepath.Join(dir, "dqlite", testControllerModelUUID))
	c.Assert(err, tc.ErrorIsNil)
	defer modelDB.Close()
	var instanceID, nonce string
	c.Assert(modelDB.QueryRow("SELECT instance_id FROM machine_cloud_instance WHERE machine_uuid = 'machine-uuid-0'").Scan(&instanceID), tc.ErrorIsNil)
	c.Check(instanceID, tc.Equals, "replacement-instance")
	c.Assert(modelDB.QueryRow("SELECT nonce FROM machine WHERE name = '0'").Scan(&nonce), tc.ErrorIsNil)
	c.Check(nonce, tc.Equals, "target-nonce")
}
