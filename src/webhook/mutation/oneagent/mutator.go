package oneagent_mutation

import (
	"context"

	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	"github.com/Dynatrace/dynatrace-operator/src/initgeneration"
	"github.com/Dynatrace/dynatrace-operator/src/kubeobjects"
	dtwebhook "github.com/Dynatrace/dynatrace-operator/src/webhook"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type OneAgentPodMutator struct {
	image     string
	clusterID string
	client    client.Client
	apiReader client.Reader

	ctx       context.Context
	dynakube  *dynatracev1beta1.DynaKube
	pod       *corev1.Pod
	namespace *corev1.Namespace
}

func NewOneAgentPodMutator(image, clusterID string, client client.Client, apiReader client.Reader) *OneAgentPodMutator {
	return &OneAgentPodMutator{
		image:     image,
		clusterID: clusterID,
		client:    client,
		apiReader: apiReader,
	}
}

func (mutator *OneAgentPodMutator) Enabled(pod *corev1.Pod) bool {
	return kubeobjects.GetFieldBool(pod.Annotations, dtwebhook.AnnotationOneAgentInject, true)
}

// func (mutator *OneAgentPodMutator) injected(pod *corev1.Pod) bool {
// 	return kubeobjects.GetFieldBool(pod.Annotations, dtwebhook.AnnotationOneAgentInjected, true)
// }

func (mutator *OneAgentPodMutator) getVolumeMode() string {
	if mutator.dynakube.NeedsCSIDriver(){
		return provisionedVolumeMode
	}
	return installerVolumeMode
}

func (mutator *OneAgentPodMutator) setState(request dtwebhook.MutationRequest) {
	mutator.ctx = request.Context
	mutator.dynakube = request.DynaKube
	mutator.pod = request.Pod
	mutator.namespace = request.Namespace
}

func (mutator *OneAgentPodMutator) Mutate(request dtwebhook.MutationRequest) error {
	mutator.setState(request)

	if err := mutator.ensureInitSecret(); err != nil {
		return err
	}
	markInjected(mutator.pod)
	mutator.addVolumes()

	installerInfo := getInstallerInfo(mutator.pod)
	mutator.configureInitContainer(request.InitContainer, installerInfo)
	mutator.updateContainers(request.InitContainer)

	return nil
}

func (mutator *OneAgentPodMutator) ensureInitSecret() error {
	var initSecret corev1.Secret
	secretObjectKey := client.ObjectKey{Name: dtwebhook.SecretConfigName, Namespace: mutator.namespace.Name}
	if err := mutator.apiReader.Get(mutator.ctx, secretObjectKey, &initSecret); k8serrors.IsNotFound(err) {
		initGenerator := initgeneration.NewInitGenerator(mutator.client, mutator.apiReader, mutator.namespace.Name)
		_, err := initGenerator.GenerateForNamespace(mutator.ctx, *mutator.dynakube, mutator.namespace.Name)
		if err != nil {
			log.Error(err, "Failed to create the init secret before oneagent pod injection")
			return err
		}
	} else if err != nil {
		log.Error(err, "failed to query the init secret before oneagent pod injection")
		return err
	}
	return nil
}


