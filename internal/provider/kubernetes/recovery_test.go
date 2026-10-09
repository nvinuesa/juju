// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"context"
	"fmt"
	"strings"
	stdtesting "testing"

	jujuerrors "github.com/juju/errors"
	"github.com/juju/tc"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/juju/juju/environs"
	envtesting "github.com/juju/juju/environs/testing"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/provider/kubernetes/constants"
	"github.com/juju/juju/internal/provider/kubernetes/exec"
	"github.com/juju/juju/internal/provider/kubernetes/utils"
)

type recoverySuite struct{}

func TestRecoverySuite(t *stdtesting.T) {
	tc.Run(t, &recoverySuite{})
}

func recoveryNamespace(name string, annotations map[string]string) *core.Namespace {
	return &core.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
	}
}

func readyControllerPod(namespace string) *core.Pod {
	return &core.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "controller-0",
			Namespace: namespace,
			Labels: utils.SelectorLabelsForApp(
				constants.JujuControllerStackName, constants.LastLabelVersion),
		},
		Status: core.PodStatus{
			Conditions: []core.PodCondition{{
				Type:   core.PodReady,
				Status: core.ConditionTrue,
			}},
		},
	}
}

func substrateParams() environs.RecoverySubstrateParams {
	return environs.RecoverySubstrateParams{
		ControllerUUID:      "ctrl-uuid",
		ControllerName:      "source-ctrl",
		ControllerModelUUID: "ctrl-model-uuid",
		Models: []environs.RecoveryModel{
			{Name: "controller", UUID: "ctrl-model-uuid"},
			{Name: "workload", UUID: "model-uuid-1"},
		},
	}
}

func ownedNamespace(c *tc.C, name, modelUUID string) *core.Namespace {
	return recoveryNamespace(name, map[string]string{
		utils.AnnotationControllerUUIDKey(constants.LastLabelVersion): "ctrl-uuid",
		utils.AnnotationModelUUIDKey(constants.LastLabelVersion):      modelUUID,
	})
}

func (s *recoverySuite) TestSubstrateHappyPath(c *tc.C) {
	client := fake.NewClientset(ownedNamespace(c, "workload", "model-uuid-1"))

	report, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(report.Empty(), tc.IsTrue)
}

func (s *recoverySuite) TestPrepareRecoveryAcceptsSurvivingWorkloadNamespaces(c *tc.C) {
	client := fake.NewClientset(
		ownedNamespace(c, "workload", "model-uuid-1"),
		&storagev1.StorageClass{ObjectMeta: v1.ObjectMeta{
			Name: "workload-storage", Annotations: map[string]string{"juju.is/workload-storage": "true"},
		}},
	)
	broker := &kubernetesClient{clientUnlocked: client, labelVersion: constants.LastLabelVersion}
	ctx := envtesting.BootstrapContext(c.Context(), c)
	report, err := broker.PrepareForRecovery(ctx, substrateParams())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(report.Empty(), tc.IsTrue)
	c.Check(broker.namespace, tc.Equals, "controller-source-ctrl")
	c.Check(report.HostCloudRegion, tc.Equals, "other")
	for _, action := range client.Actions() {
		c.Check(action.GetVerb() == "get" || action.GetVerb() == "list", tc.IsTrue)
	}
}

func (s *recoverySuite) TestSubstrateControllerNotFenced(c *tc.C) {
	client := fake.NewClientset(
		recoveryNamespace("controller-source-ctrl", nil),
		readyControllerPod("controller-source-ctrl"),
		ownedNamespace(c, "workload", "model-uuid-1"),
	)

	_, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "source controller .source-ctrl. is not fenced.*")
}

func (s *recoverySuite) TestSubstrateLeftoverControllerNamespace(c *tc.C) {
	client := fake.NewClientset(
		recoveryNamespace("controller-source-ctrl", nil),
		ownedNamespace(c, "workload", "model-uuid-1"),
	)

	_, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "leftover controller namespace .* must be deleted before recovery")
}

// TestSubstrateStrayPodNotController checks that a ready pod that is not
// a controller pod does not count as an unfenced source: it is debris in
// the leftover namespace, not a running controller.
func (s *recoverySuite) TestSubstrateStrayPodNotController(c *tc.C) {
	stray := &core.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "leftover-metrics",
			Namespace: "controller-source-ctrl",
		},
		Status: core.PodStatus{
			Conditions: []core.PodCondition{{
				Type:   core.PodReady,
				Status: core.ConditionTrue,
			}},
		},
	}
	client := fake.NewClientset(
		recoveryNamespace("controller-source-ctrl", nil),
		stray,
		ownedNamespace(c, "workload", "model-uuid-1"),
	)

	_, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "leftover controller namespace .* must be deleted before recovery")
}

func (s *recoverySuite) TestSubstrateMissingWorkloadNamespace(c *tc.C) {
	client := fake.NewClientset()

	_, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "workload namespace .workload. .* is missing.*")
}

func (s *recoverySuite) TestSubstrateForeignControllerNamespace(c *tc.C) {
	foreign := recoveryNamespace("workload", map[string]string{
		utils.AnnotationControllerUUIDKey(constants.LastLabelVersion): "other-controller",
		utils.AnnotationModelUUIDKey(constants.LastLabelVersion):      "model-uuid-1",
	})
	client := fake.NewClientset(foreign)

	_, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "workload namespace .workload. is annotated for controller .other-controller.*")
}

func (s *recoverySuite) TestSubstrateForeignModelNamespace(c *tc.C) {
	client := fake.NewClientset(ownedNamespace(c, "workload", "other-model"))

	_, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "workload namespace .workload. is annotated for model .other-model.*")
}

// substrateParamsWithApp returns substrate params whose workload model
// carries one archived application with its StatefulSet name and one
// archived volume claim.
func substrateParamsWithApp() environs.RecoverySubstrateParams {
	params := substrateParams()
	params.Models[1].Applications = []environs.RecoveryApplication{{
		Name:                   "gitlab",
		UUID:                   "app-uuid",
		Units:                  []string{"gitlab/0"},
		PersistentVolumeClaims: []string{"gitlab-appu-0"},
	}}
	return params
}

func substrateStatefulSet(namespace, name string) *apps.StatefulSet {
	return &apps.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{utils.AnnotationKeyApplicationUUID(constants.LastLabelVersion): "app-uu"},
		},
	}
}

func substratePVC(namespace, name string) *core.PersistentVolumeClaim {
	return &core.PersistentVolumeClaim{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func (s *recoverySuite) TestSubstrateReportMissingWorkloadAndClaim(c *tc.C) {
	// The namespace survives but neither the application's StatefulSet
	// nor its volume claim do: both are reported, neither is fatal.
	client := fake.NewClientset(ownedNamespace(c, "workload", "model-uuid-1"))

	report, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParamsWithApp())
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(report, tc.NotNil)
	c.Assert(report.MissingWorkloads, tc.DeepEquals, []string{"workload/gitlab"})
	c.Assert(report.MissingPersistentVolumeClaims, tc.DeepEquals, []string{"workload/gitlab-appu-0"})
	c.Check(report.Empty(), tc.IsFalse)
}

func (s *recoverySuite) TestSubstrateReportEmptyWhenSubstrateSurvives(c *tc.C) {
	client := fake.NewClientset(
		ownedNamespace(c, "workload", "model-uuid-1"),
		substrateStatefulSet("workload", "gitlab"),
		substratePVC("workload", "gitlab-appu-0"),
	)

	report, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParamsWithApp())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(report.Empty(), tc.IsTrue)
}

func (s *recoverySuite) TestForeignApplicationAbortsRecovery(c *tc.C) {
	statefulSet := substrateStatefulSet("workload", "gitlab")
	statefulSet.Annotations[utils.AnnotationKeyApplicationUUID(constants.LastLabelVersion)] = "foreign"
	client := fake.NewClientset(ownedNamespace(c, "workload", "model-uuid-1"), statefulSet)
	_, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParamsWithApp())
	c.Check(err, tc.ErrorMatches, `workload statefulset "gitlab" in "workload" belongs to another application`)
}

func (s *recoverySuite) TestCleanupUsesOnlyOwnedNamespaceUID(c *tc.C) {
	target := recoveryNamespace("controller-source-ctrl", nil)
	target.UID = "target-uid"
	client := fake.NewClientset(target, ownedNamespace(c, "workload", "model-uuid-1"))
	client.PrependReactor("delete", "namespaces", func(action clienttesting.Action) (bool, runtime.Object, error) {
		deletion := action.(clienttesting.DeleteAction)
		c.Check(deletion.GetName(), tc.Equals, "controller-source-ctrl")
		c.Assert(deletion.GetDeleteOptions().Preconditions, tc.NotNil)
		c.Assert(deletion.GetDeleteOptions().Preconditions.UID, tc.NotNil)
		c.Check(*deletion.GetDeleteOptions().Preconditions.UID, tc.Equals, target.UID)
		return false, nil, nil
	})
	broker := &kubernetesClient{clientUnlocked: client}
	cleanup := broker.recoveryNamespaceCleanup(target.Name, target.UID)
	c.Assert(cleanup(c.Context()), tc.ErrorIsNil)
	_, err := client.CoreV1().Namespaces().Get(c.Context(), "workload", v1.GetOptions{})
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(cleanup(c.Context()), tc.ErrorIsNil)
}

func (s *recoverySuite) TestServiceAccountRetainsArchivedBinding(c *tc.C) {
	annotations := map[string]string{utils.AnnotationControllerUUIDKey(constants.LastLabelVersion): "ctrl-uuid"}
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: v1.ObjectMeta{Name: "controller-source-ctrl", UID: "archived", Annotations: annotations},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "controller", Namespace: "controller-source-ctrl"}},
	}
	client := fake.NewClientset(binding)
	broker := &kubernetesClient{clientUnlocked: client, namespace: binding.Name, controllerUUID: "ctrl-uuid"}
	uid, err := broker.createRecoveryServiceAccount(c.Context(), nil, annotations)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(uid, tc.Equals, types.UID(""))
	cleanup := broker.recoveryControllerCleanup(binding.Name, "namespace-uid", &uid)
	c.Assert(cleanup(c.Context()), tc.ErrorIsNil)
	actual, err := client.RbacV1().ClusterRoleBindings().Get(c.Context(), binding.Name, v1.GetOptions{})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(actual, tc.DeepEquals, binding)
}

func (s *recoverySuite) TestServiceAccountRejectsForeignBinding(c *tc.C) {
	client := fake.NewClientset(&rbacv1.ClusterRoleBinding{ObjectMeta: v1.ObjectMeta{Name: "controller-source-ctrl"}})
	broker := &kubernetesClient{clientUnlocked: client, namespace: "controller-source-ctrl", controllerUUID: "ctrl-uuid"}
	uid, err := broker.createRecoveryServiceAccount(c.Context(), nil, nil)
	c.Check(err, tc.ErrorMatches, "existing controller cluster role binding has unexpected ownership or permissions")
	c.Check(uid, tc.Equals, types.UID(""))
	for _, action := range client.Actions() {
		if action.GetResource().Resource == "clusterrolebindings" {
			c.Check(action.GetVerb(), tc.Equals, "get")
		}
	}
}

func (s *recoverySuite) TestCleanupDeletesCreatedBindingWithUID(c *tc.C) {
	client := fake.NewClientset()
	client.PrependReactor("create", "clusterrolebindings", func(action clienttesting.Action) (bool, runtime.Object, error) {
		binding := action.(clienttesting.CreateAction).GetObject().(*rbacv1.ClusterRoleBinding)
		binding.UID = "new-binding-uid"
		return false, nil, nil
	})
	client.PrependReactor("delete", "clusterrolebindings", func(action clienttesting.Action) (bool, runtime.Object, error) {
		deletion := action.(clienttesting.DeleteAction)
		c.Check(deletion.GetName(), tc.Equals, "controller-source-ctrl")
		c.Assert(deletion.GetDeleteOptions().Preconditions, tc.NotNil)
		c.Assert(deletion.GetDeleteOptions().Preconditions.UID, tc.NotNil)
		c.Check(*deletion.GetDeleteOptions().Preconditions.UID, tc.Equals, types.UID("new-binding-uid"))
		return false, nil, nil
	})
	broker := &kubernetesClient{clientUnlocked: client, namespace: "controller-source-ctrl", controllerUUID: "ctrl-uuid"}
	uid, err := broker.createRecoveryServiceAccount(c.Context(), nil, nil)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(uid, tc.Equals, types.UID("new-binding-uid"))
	cleanup := broker.recoveryControllerCleanup(broker.namespace, "namespace-uid", &uid)
	c.Assert(cleanup(c.Context()), tc.ErrorIsNil)
	c.Assert(cleanup(c.Context()), tc.ErrorIsNil)
}

type archiveExecutor struct {
	checksum string
	commands []string
	copies   []exec.CopyParams
}

func (e *archiveExecutor) Status(context.Context, exec.StatusParams) (*exec.Status, error) {
	return nil, nil
}
func (e *archiveExecutor) RawClient() kubernetes.Interface { return nil }
func (e *archiveExecutor) NameSpace() string               { return "controller-source-ctrl" }
func (e *archiveExecutor) Exec(_ context.Context, params exec.ExecParams, _ <-chan struct{}) error {
	e.commands = append(e.commands, strings.Join(params.Commands, " "))
	if params.Commands[0] == "sha256sum" {
		_, err := fmt.Fprintln(params.Stdout, e.checksum, params.Commands[1])
		return err
	}
	return nil
}
func (e *archiveExecutor) Copy(_ context.Context, params exec.CopyParams, _ <-chan struct{}) error {
	e.copies = append(e.copies, params)
	return nil
}

func (s *recoverySuite) TestArchiveIsPublishedOnlyAfterChecksumVerification(c *tc.C) {
	for _, valid := range []bool{true, false} {
		file := instancecfg.InitialisationFile{Source: "/tmp/source.tar.gz", Destination: "/var/lib/juju/recovery/archive.tar.gz", SHA256: strings.Repeat("a", 64)}
		client := &archiveExecutor{checksum: file.SHA256}
		if !valid {
			client.checksum = strings.Repeat("b", 64)
		}
		stack := &controllerStack{controllerExecClientFactory: func() (exec.Executor, error) { return client, nil }}
		err := stack.uploadRecoveryArchive(c.Context(), "controller-0", file)
		if valid {
			c.Assert(err, tc.ErrorIsNil)
			c.Check(client.commands[len(client.commands)-1], tc.Equals, "mv -f "+file.Destination+".partial "+file.Destination)
		} else {
			c.Check(err, tc.ErrorIs, jujuerrors.NotValid)
			for _, command := range client.commands {
				c.Check(command, tc.Not(tc.HasPrefix), "mv ")
			}
		}
		c.Check(client.commands[1], tc.Equals, "chmod 0700 /var/lib/juju/recovery")
		c.Assert(client.copies, tc.HasLen, 1)
		c.Check(client.copies[0].Src.Path, tc.Equals, file.Source)
		c.Check(client.copies[0].Dest.Path, tc.Equals, file.Destination+".partial")
		c.Check(client.copies[0].Dest.ContainerName, tc.Equals, "api-server")
	}
}
