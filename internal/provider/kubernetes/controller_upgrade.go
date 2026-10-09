// Copyright 2020 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package kubernetes

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/distribution/reference"
	"github.com/juju/errors"
	appsv1 "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/juju/juju/core/semversion"
	"github.com/juju/juju/environs/bootstrap"
	"github.com/juju/juju/internal/cloudconfig/podcfg"
	"github.com/juju/juju/internal/docker"
	"github.com/juju/juju/internal/docker/registry"
	"github.com/juju/juju/internal/provider/kubernetes/constants"
	"github.com/juju/juju/internal/provider/kubernetes/resources"
	providerutils "github.com/juju/juju/internal/provider/kubernetes/utils"
)

type upgradeCAASControllerBridge struct {
	clientFn       func() kubernetes.Interface
	labelVersionFn func() constants.LabelVersion
	namespaceFn    func() string
}

// UpgradeCAASControllerBroker describes the interface needed for upgrading
// Juju Kubernetes controllers
type UpgradeCAASControllerBroker interface {
	// Client returns a Kubernetes client associated with the current broker's
	// cluster
	Client() kubernetes.Interface

	// LabelVersion returns the detected label version for k8s resources created
	// for this model.
	LabelVersion() constants.LabelVersion

	// Namespace returns the targeted Kubernetes namespace for this broker
	Namespace() string
}

func (u *upgradeCAASControllerBridge) Client() kubernetes.Interface {
	return u.clientFn()
}

func (u *upgradeCAASControllerBridge) LabelVersion() constants.LabelVersion {
	return u.labelVersionFn()
}

func (u *upgradeCAASControllerBridge) Namespace() string {
	return u.namespaceFn()
}

func controllerUpgrade(ctx context.Context, appName string, vers semversion.Number, image string, broker UpgradeCAASControllerBroker) error {
	return upgradeOperatorOrControllerStatefulSet(
		ctx,
		appName,
		image,
		vers,
		broker.LabelVersion(),
		broker.Client().AppsV1().StatefulSets(broker.Namespace()))
}

func (k *kubernetesClient) upgradeController(ctx context.Context, vers semversion.Number) error {
	broker := &upgradeCAASControllerBridge{
		clientFn:       k.client,
		namespaceFn:    k.Namespace,
		labelVersionFn: k.LabelVersion,
	}
	set, err := k.client().AppsV1().StatefulSets(k.Namespace()).Get(ctx, bootstrap.ControllerModelName, metav1.GetOptions{})
	if err != nil {
		return errors.Trace(err)
	}
	index, found := findJujudContainer(set.Spec.Template.Spec.Containers)
	if !found {
		return errors.New("controller has no agent container")
	}
	image, err := podcfg.RebuildOldOperatorImagePath(set.Spec.Template.Spec.Containers[index].Image, vers)
	if err != nil {
		return err
	}
	architecture := set.Spec.Template.Spec.NodeSelector["kubernetes.io/arch"]
	if architecture == "" {
		architecture = set.Spec.Template.Spec.NodeSelector["beta.kubernetes.io/arch"]
	}
	if architecture == "" {
		architecture = "amd64"
	}
	details, err := k.controllerImageCredentials(ctx, image)
	if err != nil {
		return err
	}
	image, err = registry.ResolveReference(ctx, image, details, architecture)
	if err != nil {
		return errors.Annotate(err, "pinning upgraded controller agent image")
	}
	return controllerUpgrade(ctx, bootstrap.ControllerModelName, vers, image, broker)
}

// InClusterCredentialUpgrade implements upgrades.upgradeKubernetesClusterCredential
// used in the Juju 2.9.6 upgrade step
func (k *kubernetesClient) InClusterCredentialUpgrade(ctx context.Context) error {
	return inClusterCredentialUpgrade(
		ctx,
		k.client(),
		k.extendedClient(),
		k.LabelVersion(),
		k.Namespace(),
		k.ControllerUUID(),
	)
}

func inClusterCredentialUpgrade(
	ctx context.Context,
	client kubernetes.Interface,
	extendedClient clientset.Interface,
	labelVersion constants.LabelVersion,
	namespace string,
	controllerUUID string,
) error {
	labels := providerutils.LabelsForApp("controller", labelVersion)

	saName, cleanUps, err := ensureControllerServiceAccount(
		ctx,
		client,
		namespace,
		controllerUUID,
		labels,
		map[string]string{},
	)

	runCleanups := func() {
		for _, v := range cleanUps {
			v()
		}
	}

	if err != nil {
		runCleanups()
		return errors.Trace(err)
	}

	ss := resources.NewStatefulSet(client.AppsV1().StatefulSets(namespace), namespace, "controller", &appsv1.StatefulSet{})
	if err := ss.Get(ctx); err != nil {
		runCleanups()
		return errors.Annotate(err, "updating controller for in cluster credentials")
	}

	ss.Spec.Template.Spec.ServiceAccountName = saName
	ss.Spec.Template.Spec.AutomountServiceAccountToken = new(true)
	if err := ss.Apply(ctx); err != nil {
		runCleanups()
		return errors.Annotate(err, "updating controller for in cluster credentials")
	}

	return nil
}

// controllerImageCredentials reads the credentials already used for image
// pulls; private registry credentials stay scoped to their original domain.
func (k *kubernetesClient) controllerImageCredentials(ctx context.Context, image string) (docker.ImageRepoDetails, error) {
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return docker.ImageRepoDetails{}, err
	}
	secret, err := k.client().CoreV1().Secrets(k.Namespace()).Get(ctx, constants.CAASImageRepoSecretName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return docker.ImageRepoDetails{}, nil
	}
	if err != nil {
		return docker.ImageRepoDetails{}, err
	}
	var config struct {
		Auths map[string]docker.ImageRepoDetails `json:"auths"`
	}
	if err := json.Unmarshal(secret.Data[core.DockerConfigJsonKey], &config); err != nil {
		return docker.ImageRepoDetails{}, err
	}
	for server, details := range config.Auths {
		domain := strings.TrimPrefix(strings.TrimPrefix(server, "https://"), "http://")
		domain = strings.TrimSuffix(domain, "/v1/")
		if domain != reference.Domain(ref) {
			continue
		}
		details.Repository = ref.Name()
		return details, nil
	}
	return docker.ImageRepoDetails{}, errors.New("controller image pull secret has no credentials for its registry")
}
