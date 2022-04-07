package oneagent_mutation

import (
	"github.com/Dynatrace/dynatrace-operator/src/kubeobjects"
	dtwebhook "github.com/Dynatrace/dynatrace-operator/src/webhook"
	corev1 "k8s.io/api/core/v1"
)

func (mutator *OneAgentPodMutator) configureInitContainer(initContainer *corev1.Container, installer installerInfo) {
	addInstallerInitEnvs(initContainer, installer, mutator.getVolumeMode())
	addInitVolumeMounts(initContainer)
}

func (mutator *OneAgentPodMutator) updateContainers(initContainer *corev1.Container) {
	for i := range mutator.pod.Spec.Containers {
		container := &mutator.pod.Spec.Containers[i]
		addContainerInfoInitEnv(initContainer, i+1, container.Name, container.Image)
		mutator.addOneAgentToContainer(container)
	}
}

func (mutator *OneAgentPodMutator) addOneAgentToContainer(container *corev1.Container) {

	log.Info("updating container with missing preload variables", "containerName", container.Name)
	installPath := kubeobjects.GetField(mutator.pod.Annotations, dtwebhook.AnnotationInstallPath, dtwebhook.DefaultInstallPath)

	addOneAgentVolumeMounts(container, installPath)
	if mutator.dynakube.HasActiveGateCaCert() {
		addCertVolumeMounts(container)
	}

	addDeploymentMetadataEnv(container, mutator.dynakube, mutator.clusterID)
	addPreloadEnv(container, installPath)
	if mutator.dynakube.HasProxy() {
		addProxyEnv(container)
	}

	if mutator.dynakube.Spec.NetworkZone != "" {
		addNetworkZoneEnv(container, mutator.dynakube.Spec.NetworkZone)
	}
}

