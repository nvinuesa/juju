// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"bytes"
	"context"
	"path"
	"reflect"
	"strings"
	"time"

	jujuerrors "github.com/juju/errors"
	"github.com/juju/retry"
	core "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/juju/juju/caas"
	"github.com/juju/juju/environs"
	environsbootstrap "github.com/juju/juju/environs/bootstrap"
	"github.com/juju/juju/internal/cloudconfig/instancecfg"
	"github.com/juju/juju/internal/cloudconfig/podcfg"
	"github.com/juju/juju/internal/errors"
	"github.com/juju/juju/internal/provider/kubernetes/constants"
	k8sexec "github.com/juju/juju/internal/provider/kubernetes/exec"
	"github.com/juju/juju/internal/provider/kubernetes/utils"
)

// ProvisionRecoveryController implements caas.RecoveryControllerProvisioner.
func (k *kubernetesClient) ProvisionRecoveryController(ctx environs.BootstrapContext, params caas.RecoveryControllerParams) (func(context.Context) error, error) {
	cfg := params.PodConfig
	if cfg == nil || cfg.Initialisation == nil || !strings.Contains(cfg.AgentImage, "@sha256:") {
		return nil, errors.New("recovery requires initialisation and an immutable agent image")
	}
	if k.namespace != DecideControllerNamespace(cfg.ControllerName) {
		return nil, errors.New("provider has not been prepared for recovery")
	}
	podcfg.FinishControllerPodConfig(cfg, k.Config(), nil)
	if err := cfg.VerifyConfig(); err != nil {
		return nil, errors.Capture(err)
	}
	storageClass, err := k.validateControllerWorkloadStorage(ctx)
	if err != nil {
		return nil, errors.Capture(err)
	}
	stack, err := makeControllerStack(ctx, constants.JujuControllerStackName, storageClass, k, cfg)
	if err != nil {
		return nil, errors.Capture(err)
	}
	// This Create is exclusive. A preflight Get alone cannot authorise cleanup
	// of a namespace another recovery created in between those operations.
	namespace := &core.Namespace{ObjectMeta: metav1.ObjectMeta{Name: k.namespace}}
	namespace.Labels = utils.LabelsMerge(utils.LabelsForModel(k.ModelName(), k.ModelUUID(), k.ControllerUUID(), k.LabelVersion()), utils.LabelsJuju)
	_ = k.addAnnotations(utils.AnnotationControllerIsControllerKey(k.LabelVersion()), "true")
	if err := k.ensureNamespaceAnnotations(namespace); err != nil {
		return nil, errors.Capture(err)
	}
	created, err := k.client().CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if err != nil {
		return nil, errors.Errorf("creating replacement namespace: %w", err)
	}
	var bindingUID types.UID
	cleanup := k.recoveryControllerCleanup(created.Name, created.UID, &bindingUID)
	stack.serviceAccountCreator = func(ctx context.Context) (string, []func(), error) {
		uid, err := k.createRecoveryServiceAccount(ctx, stack.stackLabels, stack.stackAnnotations)
		bindingUID = uid
		return constants.JujuControllerStackName, nil, err
	}
	if err := stack.deployResources(ctx); err != nil {
		return cleanup, errors.Capture(err)
	}
	err = retry.Call(retry.CallArgs{
		Attempts: 12, Delay: time.Second, Stop: ctx.Done(), Clock: k.clock,
		Func:         func() error { return stack.uploadRecoveryArchive(ctx, cfg.GetPodName(), params.Archive) },
		IsFatalError: func(err error) bool { return errors.Is(err, jujuerrors.NotValid) },
	})
	return cleanup, errors.Capture(err)
}

func (k *kubernetesClient) recoveryControllerCleanup(name string, namespaceUID types.UID, bindingUID *types.UID) func(context.Context) error {
	namespaceCleanup := k.recoveryNamespaceCleanup(name, namespaceUID)
	return func(ctx context.Context) error {
		var bindingErr error
		if *bindingUID != "" {
			bindingErr = k.client().RbacV1().ClusterRoleBindings().Delete(ctx,
				name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: bindingUID}})
			if k8serrors.IsNotFound(bindingErr) {
				bindingErr = nil
			}
		}
		return errors.Join(bindingErr, namespaceCleanup(ctx))
	}
}

// createRecoveryServiceAccount leaves an archived binding untouched. Only an
// exclusive Create authorises deletion of a binding during target cleanup.
func (k *kubernetesClient) createRecoveryServiceAccount(ctx context.Context, labels, annotations map[string]string) (types.UID, error) {
	_, err := k.client().CoreV1().ServiceAccounts(k.namespace).Create(ctx, &core.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: constants.JujuControllerStackName,
			Namespace:   k.namespace,
			Labels:      utils.LabelsMerge(labels, utils.LabelsJujuModelOperatorDisableWebhook),
			Annotations: annotations},
		AutomountServiceAccountToken: new(true),
	}, metav1.CreateOptions{})
	if err != nil {
		return "", errors.Capture(err)
	}
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: k.namespace,
			Labels:      utils.LabelsForModel(environsbootstrap.ControllerModelName, "", k.ControllerUUID(), constants.LastLabelVersion),
			Annotations: annotations},
		RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: constants.JujuControllerStackName, Namespace: k.namespace}},
	}
	existing, err := k.client().RbacV1().ClusterRoleBindings().Get(ctx, binding.Name, metav1.GetOptions{})
	if err == nil {
		key := utils.AnnotationControllerUUIDKey(constants.LastLabelVersion)
		if existing.Annotations[key] != k.ControllerUUID() || existing.RoleRef != binding.RoleRef || !reflect.DeepEqual(existing.Subjects, binding.Subjects) {
			return "", errors.New("existing controller cluster role binding has unexpected ownership or permissions")
		}
		return "", nil
	}
	if !k8serrors.IsNotFound(err) {
		return "", errors.Capture(err)
	}
	created, err := k.client().RbacV1().ClusterRoleBindings().Create(ctx, binding, metav1.CreateOptions{})
	if err != nil {
		return "", errors.Capture(err)
	}
	return created.UID, nil
}

func (k *kubernetesClient) recoveryNamespaceCleanup(name string, uid types.UID) func(context.Context) error {
	return func(ctx context.Context) error {
		err := k.client().CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return errors.Capture(err)
	}
}

func (c *controllerStack) uploadRecoveryArchive(ctx context.Context, podName string, file instancecfg.InitialisationFile) error {
	client, err := c.controllerExecClientFactory()
	if err != nil {
		return errors.Capture(err)
	}
	run := func(commands []string) (string, error) {
		var stdout, stderr bytes.Buffer
		err := client.Exec(ctx, k8sexec.ExecParams{PodName: podName, ContainerName: apiServerContainerName, Commands: commands, Stdout: &stdout, Stderr: &stderr}, nil)
		if err != nil {
			return "", errors.Errorf("transferring recovery archive: %w", err)
		}
		return stdout.String(), nil
	}
	if _, err := run([]string{"mkdir", "-p", path.Dir(file.Destination)}); err != nil {
		return err
	}
	if _, err := run([]string{"chmod", "0700", path.Dir(file.Destination)}); err != nil {
		return err
	}
	partial := file.Destination + ".partial"
	if err := client.Copy(ctx, k8sexec.CopyParams{Src: k8sexec.FileResource{Path: file.Source}, Dest: k8sexec.FileResource{Path: partial, PodName: podName, ContainerName: apiServerContainerName}}, nil); err != nil {
		return errors.Capture(err)
	}
	out, err := run([]string{"sha256sum", partial})
	if err != nil {
		return err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || !strings.EqualFold(fields[0], file.SHA256) {
		return errors.New("uploaded recovery archive checksum mismatch").Add(jujuerrors.NotValid)
	}
	if _, err := run([]string{"chmod", "0600", partial}); err != nil {
		return err
	}
	_, err = run([]string{"mv", "-f", partial, file.Destination})
	return err
}
