package oneagent_mutation

import (
	"fmt"

	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/oneagent/daemonset"
	"github.com/Dynatrace/dynatrace-operator/src/deploymentmetadata"
	"github.com/Dynatrace/dynatrace-operator/src/standalone"
	dtwebhook "github.com/Dynatrace/dynatrace-operator/src/webhook"
	corev1 "k8s.io/api/core/v1"
)

func addPreloadEnv(container *corev1.Container, installPath string) {
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name:  "LD_PRELOAD",
			Value: installPath + "/agent/lib64/liboneagentproc.so",
		})
}

func addNetworkZoneEnv(container *corev1.Container, networkZone string) {
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name: "DT_NETWORK_ZONE",
			Value: networkZone,
		},
	)
}


func addProxyEnv(container *corev1.Container) {
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name: "DT_PROXY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: dtwebhook.SecretConfigName,
					},
					Key: "proxy",
				},
			},
		})
}

func addInstallerInitEnvs(initContainer *corev1.Container, installer installerInfo, volumeMode string) {
	initContainer.Env = append(initContainer.Env,
		corev1.EnvVar{Name: standalone.InstallerFlavorEnv, Value: installer.flavor},
		corev1.EnvVar{Name: standalone.InstallerTechEnv, Value: installer.technologies},
		corev1.EnvVar{Name: standalone.InstallPathEnv, Value: installer.installPath},
		corev1.EnvVar{Name: standalone.InstallerUrlEnv, Value: installer.installerURL},
		corev1.EnvVar{Name: standalone.ModeEnv, Value: volumeMode},
		corev1.EnvVar{Name: standalone.OneAgentInjectedEnv, Value: "true"},
	)
}

func addContainerInfoInitEnv(initContainer *corev1.Container, number int, name string, image string) {
	log.Info("updating init container with new container", "containerName", name, "containerImage", image)
	initContainer.Env = append(initContainer.Env,
		corev1.EnvVar{Name: fmt.Sprintf("CONTAINER_%d_NAME", number), Value: name},
		corev1.EnvVar{Name: fmt.Sprintf("CONTAINER_%d_IMAGE", number), Value: image})
}

func addDeploymentMetadataEnv(container *corev1.Container, dynakube *dynatracev1beta1.DynaKube, clusterID string) {
	var deploymentMetadata *deploymentmetadata.DeploymentMetadata
	if dynakube.CloudNativeFullstackMode() {
		deploymentMetadata = deploymentmetadata.NewDeploymentMetadata(clusterID, daemonset.DeploymentTypeCloudNative)
	} else {
		deploymentMetadata = deploymentmetadata.NewDeploymentMetadata(clusterID, daemonset.DeploymentTypeApplicationMonitoring)
	}
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name:  dynatraceMetadataEnvVarName,
			Value: deploymentMetadata.AsString(),
		})
}
