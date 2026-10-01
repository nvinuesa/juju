// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	stdtesting "testing"

	"github.com/juju/tc"
	core "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/internal/provider/kubernetes/constants"
	"github.com/juju/juju/internal/provider/kubernetes/utils"
)

type restoreSuite struct{}

func TestRestoreSuite(t *stdtesting.T) {
	tc.Run(t, &restoreSuite{})
}

func restoreNamespace(name string, annotations map[string]string) *core.Namespace {
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

func substrateParams() environs.RestoreSubstrateParams {
	return environs.RestoreSubstrateParams{
		ControllerUUID:      "ctrl-uuid",
		ControllerName:      "source-ctrl",
		ControllerModelUUID: "ctrl-model-uuid",
		Models: []environs.RestoreModel{
			{Name: "controller", UUID: "ctrl-model-uuid"},
			{Name: "workload", UUID: "model-uuid-1"},
		},
	}
}

func ownedNamespace(c *tc.C, name, modelUUID string) *core.Namespace {
	return restoreNamespace(name, map[string]string{
		utils.AnnotationControllerUUIDKey(constants.LastLabelVersion): "ctrl-uuid",
		utils.AnnotationModelUUIDKey(constants.LastLabelVersion):      modelUUID,
	})
}

func (s *restoreSuite) TestSubstrateHappyPath(c *tc.C) {
	client := fake.NewClientset(ownedNamespace(c, "workload", "model-uuid-1"))

	err := CheckRestoreSubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorIsNil)
}

func (s *restoreSuite) TestSubstrateControllerNotFenced(c *tc.C) {
	client := fake.NewClientset(
		restoreNamespace("controller-source-ctrl", nil),
		readyControllerPod("controller-source-ctrl"),
		ownedNamespace(c, "workload", "model-uuid-1"),
	)

	err := CheckRestoreSubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "source controller .source-ctrl. is not fenced.*")
}

func (s *restoreSuite) TestSubstrateLeftoverControllerNamespace(c *tc.C) {
	client := fake.NewClientset(
		restoreNamespace("controller-source-ctrl", nil),
		ownedNamespace(c, "workload", "model-uuid-1"),
	)

	err := CheckRestoreSubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "leftover controller namespace .* must be deleted before restore")
}

// TestSubstrateStrayPodNotController checks that a ready pod that is not
// a controller pod does not count as an unfenced source: it is debris in
// the leftover namespace, not a running controller.
func (s *restoreSuite) TestSubstrateStrayPodNotController(c *tc.C) {
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
		restoreNamespace("controller-source-ctrl", nil),
		stray,
		ownedNamespace(c, "workload", "model-uuid-1"),
	)

	err := CheckRestoreSubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "leftover controller namespace .* must be deleted before restore")
}

func (s *restoreSuite) TestSubstrateMissingWorkloadNamespace(c *tc.C) {
	client := fake.NewClientset()

	err := CheckRestoreSubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "workload namespace .workload. .* is missing.*")
}

func (s *restoreSuite) TestSubstrateForeignControllerNamespace(c *tc.C) {
	foreign := restoreNamespace("workload", map[string]string{
		utils.AnnotationControllerUUIDKey(constants.LastLabelVersion): "other-controller",
		utils.AnnotationModelUUIDKey(constants.LastLabelVersion):      "model-uuid-1",
	})
	client := fake.NewClientset(foreign)

	err := CheckRestoreSubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "workload namespace .workload. is annotated for controller .other-controller.*")
}

func (s *restoreSuite) TestSubstrateForeignModelNamespace(c *tc.C) {
	client := fake.NewClientset(ownedNamespace(c, "workload", "other-model"))

	err := CheckRestoreSubstrate(c.Context(), client, constants.LastLabelVersion, substrateParams())
	c.Assert(err, tc.ErrorMatches, "workload namespace .workload. is annotated for model .other-model.*")
}
