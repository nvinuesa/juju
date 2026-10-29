// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"context"
	"strings"
	"testing"
	"time"

	testclock "github.com/juju/clock/testclock"
	jujuerrors "github.com/juju/errors"
	"github.com/juju/tc"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/juju/juju/caas"
	envtesting "github.com/juju/juju/environs/testing"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/cloudconfig/podcfg"
)

type recoveryImageSuite struct{}

func TestRecoveryImageSuite(t *testing.T) { tc.Run(t, &recoveryImageSuite{}) }

func (s *recoveryImageSuite) TestImagesValidatedBeforeProviderPreparation(c *tc.C) {
	for _, test := range []struct{ agent, charm, message string }{
		{"", "repo/charm:tag", "invalid recovery controller agent image:.*"},
		{"bad image", "repo/charm:tag", "invalid recovery controller agent image:.*"},
		{"repo/agent:tag", "", "invalid recovery controller charm image:.*"},
		{"repo/agent:tag", "bad image", "invalid recovery controller charm image:.*"},
		{"repo/agent:tag", "repo/charm:tag", "provider has not been prepared for recovery"},
		{"repo/agent@sha256:" + strings.Repeat("a", 64), "repo/charm@sha256:" + strings.Repeat("b", 64), "provider has not been prepared for recovery"},
	} {
		client := fake.NewClientset()
		broker := &kubernetesClient{clientUnlocked: client}
		cfg := &podcfg.ControllerPodConfig{
			AgentImage: test.agent, CharmImage: test.charm,
			Initialisation: &podcfg.ControllerInitialisation{},
			Bootstrap: &podcfg.BootstrapConfig{BootstrapConfig: instancecfg.BootstrapConfig{Timeout: time.Minute}},
		}
		cleanup, err := broker.ProvisionRecoveryController(envtesting.BootstrapContext(c.Context(), c), caas.RecoveryControllerParams{PodConfig: cfg})
		c.Check(err, tc.ErrorMatches, test.message)
		c.Check(cleanup, tc.IsNil)
		c.Check(client.Actions(), tc.HasLen, 0)
	}
}

func waitingStatus(name, reason, message string) core.ContainerStatus {
	return core.ContainerStatus{Name: name, State: core.ContainerState{Waiting: &core.ContainerStateWaiting{Reason: reason, Message: message}}}
}

func (s *recoveryImageSuite) TestWaitReportsConcreteContainerFailures(c *tc.C) {
	for _, test := range []struct {
		init, regular []core.ContainerStatus
		message string
	}{
		{[]core.ContainerStatus{waitingStatus("setup", "InvalidImageName", "invalid tag")}, []core.ContainerStatus{waitingStatus(apiServerContainerName, "PodInitializing", "")}, `recovery container "setup" cannot start: InvalidImageName: invalid tag`},
		{[]core.ContainerStatus{waitingStatus("setup", "ContainerCreating", "")}, []core.ContainerStatus{waitingStatus("charm", "CreateContainerConfigError", "missing secret")}, `recovery container "charm" cannot start: CreateContainerConfigError: missing secret`},
	} {
		pod := &core.Pod{ObjectMeta: metav1.ObjectMeta{Name: "controller-0", Namespace: "controller-test"}, Status: core.PodStatus{InitContainerStatuses: test.init, ContainerStatuses: test.regular}}
		client := fake.NewClientset(pod)
		clk := testclock.NewClock(time.Time{})
		broker := &kubernetesClient{clientUnlocked: client, namespace: pod.Namespace, clock: clk}
		err := broker.waitForRecoveryContainer(c.Context(), pod.Name, time.Minute)
		c.Check(err, tc.ErrorMatches, test.message)
		c.Check(err, tc.ErrorIs, jujuerrors.NotValid)
		c.Check(client.Actions(), tc.HasLen, 1)
	}
}

func (s *recoveryImageSuite) TestRunningAPIWinsOverOtherWaitingStatuses(c *tc.C) {
	pod := &core.Pod{ObjectMeta: metav1.ObjectMeta{Name: "controller-0", Namespace: "controller-test"}, Status: core.PodStatus{
		InitContainerStatuses: []core.ContainerStatus{waitingStatus("setup", "InvalidImageName", "bad")},
		ContainerStatuses: []core.ContainerStatus{{Name: apiServerContainerName, Ready: false, State: core.ContainerState{Running: &core.ContainerStateRunning{}}}},
	}}
	broker := &kubernetesClient{clientUnlocked: fake.NewClientset(pod), namespace: pod.Namespace, clock: testclock.NewClock(time.Time{})}
	c.Assert(broker.waitForRecoveryContainer(c.Context(), pod.Name, time.Minute), tc.ErrorIsNil)
}

func (s *recoveryImageSuite) TestImagePullFailureRetriesUntilContainerRuns(c *tc.C) {
	for _, reason := range []string{"ErrImagePull", "ImagePullBackOff"} {
		client := fake.NewClientset()
		calls := 0
		client.PrependReactor("get", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
			calls++
			status := waitingStatus("setup", reason, "not yet available")
			if calls > 1 {
				status = core.ContainerStatus{Name: apiServerContainerName, State: core.ContainerState{Running: &core.ContainerStateRunning{}}}
			}
			return true, &core.Pod{Status: core.PodStatus{ContainerStatuses: []core.ContainerStatus{status}}}, nil
		})
		clk := testclock.NewClock(time.Time{})
		broker := &kubernetesClient{clientUnlocked: client, namespace: "controller-test", clock: &testclock.AutoAdvancingClock{Clock: clk, Advance: clk.Advance}}
		c.Assert(broker.waitForRecoveryContainer(c.Context(), "controller-0", time.Minute), tc.ErrorIsNil)
		c.Check(calls, tc.Equals, 2)
	}
}

func (s *recoveryImageSuite) TestWaitHonoursCancellation(c *tc.C) {
	ctx, cancel := context.WithCancel(c.Context())
	cancel()
	broker := &kubernetesClient{clientUnlocked: fake.NewClientset(), clock: testclock.NewClock(time.Time{})}
	c.Check(broker.waitForRecoveryContainer(ctx, "controller-0", time.Minute), tc.ErrorIs, context.Canceled)
}

func (s *recoveryImageSuite) TestWaitReportsPullFailureAtDeadline(c *tc.C) {
	for _, test := range []struct{ reason, message string }{
		{"ErrImagePull", `.*recovery container "setup" cannot start: ErrImagePull: unavailable.*`},
		{"ImagePullBackOff", `.*recovery container "setup" cannot start: ImagePullBackOff: unavailable.*`},
		{"PodInitializing", `.*waiting for recovery container to start.*`},
	} {
		pod := &core.Pod{ObjectMeta: metav1.ObjectMeta{Name: "controller-0", Namespace: "controller-test"}, Status: core.PodStatus{
			InitContainerStatuses: []core.ContainerStatus{waitingStatus("setup", test.reason, "unavailable")},
		}}
		client := fake.NewClientset(pod)
		clk := testclock.NewClock(time.Time{})
		broker := &kubernetesClient{clientUnlocked: client, namespace: pod.Namespace, clock: &testclock.AutoAdvancingClock{Clock: clk, Advance: clk.Advance}}
		err := broker.waitForRecoveryContainer(c.Context(), pod.Name, time.Second)
		c.Check(err, tc.ErrorMatches, test.message)
		c.Check(err, tc.Not(tc.ErrorIs), jujuerrors.NotValid)
		c.Check(client.Actions(), tc.HasLen, 2)
	}
}

func (s *recoveryImageSuite) TestCancellationDuringRetry(c *tc.C) {
	ctx, cancel := context.WithCancel(c.Context())
	client := fake.NewClientset()
	client.PrependReactor("get", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		cancel()
		return true, &core.Pod{Status: core.PodStatus{ContainerStatuses: []core.ContainerStatus{
			waitingStatus(apiServerContainerName, "ErrImagePull", "unavailable"),
		}}}, nil
	})
	broker := &kubernetesClient{clientUnlocked: client, clock: testclock.NewClock(time.Time{})}
	c.Check(broker.waitForRecoveryContainer(ctx, "controller-0", time.Minute), tc.ErrorIs, context.Canceled)
}

func (s *recoveryImageSuite) TestProvisionRequiresInitialisation(c *tc.C) {
	broker := &kubernetesClient{}
	ctx := envtesting.BootstrapContext(c.Context(), c)
	for _, cfg := range []*podcfg.ControllerPodConfig{
		nil,
		{},
		{Initialisation: &podcfg.ControllerInitialisation{}},
		{Initialisation: &podcfg.ControllerInitialisation{}, Bootstrap: &podcfg.BootstrapConfig{}},
	} {
		_, err := broker.ProvisionRecoveryController(ctx, caas.RecoveryControllerParams{PodConfig: cfg})
		c.Check(err, tc.ErrorMatches, "recovery requires initialisation and controller images")
	}
}
