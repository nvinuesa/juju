// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"fmt"
	stdtesting "testing"

	"github.com/juju/tc"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/juju/juju/environs"
	envtesting "github.com/juju/juju/environs/testing"
	"github.com/juju/juju/internal/provider/kubernetes/constants"
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

func (s *recoverySuite) TestPrepareRecoveryNodeListError(c *tc.C) {
	client := fake.NewClientset(ownedNamespace(c, "workload", "model-uuid-1"))
	client.PrependReactor("list", "nodes", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("node list unavailable")
	})
	broker := &kubernetesClient{clientUnlocked: client, labelVersion: constants.LastLabelVersion}
	_, err := broker.PrepareForRecovery(envtesting.BootstrapContext(c.Context(), c), substrateParams())
	c.Assert(err, tc.ErrorMatches, ".*node list unavailable.*")
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

func (s *recoverySuite) TestForeignApplicationAbortsRecovery(c *tc.C) {
	statefulSet := substrateStatefulSet("workload", "gitlab")
	statefulSet.Annotations[utils.AnnotationKeyApplicationUUID(constants.LastLabelVersion)] = "foreign"
	client := fake.NewClientset(ownedNamespace(c, "workload", "model-uuid-1"), statefulSet)
	_, err := CheckRecoverySubstrate(c.Context(), client, constants.LastLabelVersion, substrateParamsWithApp())
	c.Check(err, tc.ErrorMatches, `workload statefulset "gitlab" in "workload" belongs to another application`)
}
