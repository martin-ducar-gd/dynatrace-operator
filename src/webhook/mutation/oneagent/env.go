package oneagent_mutation

import (
	"github.com/Dynatrace/dynatrace-operator/src/standalone"
	corev1 "k8s.io/api/core/v1"
)


func decorateInstallContainerWithOneAgent(ic *corev1.Container, flavor string, technologies string, installPath string, installerURL string, mode string) {
	ic.Env = append(ic.Env,
		corev1.EnvVar{Name: standalone.InstallerFlavorEnv, Value: flavor},
		corev1.EnvVar{Name: standalone.InstallerTechEnv, Value: technologies},
		corev1.EnvVar{Name: standalone.InstallPathEnv, Value: installPath},
		corev1.EnvVar{Name: standalone.InstallerUrlEnv, Value: installerURL},
		corev1.EnvVar{Name: standalone.ModeEnv, Value: mode},
		corev1.EnvVar{Name: standalone.OneAgentInjectedEnv, Value: "true"},
	)

	ic.VolumeMounts = append(ic.VolumeMounts,
		corev1.VolumeMount{Name: oneAgentBinVolumeName, MountPath: standalone.BinDirMount},
		corev1.VolumeMount{Name: oneAgentShareVolumeName, MountPath: standalone.ShareDirMount},
	)
}

func updateContainers(pod *corev1.Pod, ic *corev1.Container, dk dynatracev1beta1.DynaKube, deploymentMetadata *deploymentmetadata.DeploymentMetadata) {
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		updateInstallContainerOneAgent(ic, i+1, c.Name, c.Image)
		updateContainerOneAgent(c, &dk, pod, deploymentMetadata)
	}
}

// updateInstallContainerOA adds Container to list of Containers of Install Container
func updateInstallContainerOneAgent(ic *corev1.Container, number int, name string, image string) {
	podLog.Info("updating install container with new container", "containerName", name, "containerImage", image)
	ic.Env = append(ic.Env,
		corev1.EnvVar{Name: fmt.Sprintf("CONTAINER_%d_NAME", number), Value: name},
		corev1.EnvVar{Name: fmt.Sprintf("CONTAINER_%d_IMAGE", number), Value: image})
}

// updateContainerOA sets missing preload Variables
func updateContainerOneAgent(c *corev1.Container, dk *dynatracev1beta1.DynaKube, pod *corev1.Pod, deploymentMetadata *deploymentmetadata.DeploymentMetadata) {

	podLog.Info("updating container with missing preload variables", "containerName", c.Name)
	installPath := kubeobjects.GetField(pod.Annotations, dtwebhook.AnnotationInstallPath, dtwebhook.DefaultInstallPath)

	addMetadataIfMissing(c, deploymentMetadata)

	c.VolumeMounts = append(c.VolumeMounts,
		corev1.VolumeMount{
			Name:      oneAgentShareVolumeName,
			MountPath: "/etc/ld.so.preload",
			SubPath:   "ld.so.preload",
		},
		corev1.VolumeMount{
			Name:      oneAgentBinVolumeName,
			MountPath: installPath,
		},
		corev1.VolumeMount{
			Name:      oneAgentShareVolumeName,
			MountPath: "/var/lib/dynatrace/oneagent/agent/config/container.conf",
			SubPath:   fmt.Sprintf(standalone.ContainerConfFilenameTemplate, c.Name),
		})
	if dk.HasActiveGateCaCert() {
		c.VolumeMounts = append(c.VolumeMounts,
			corev1.VolumeMount{
				Name:      oneAgentShareVolumeName,
				MountPath: filepath.Join(oneAgentCustomKeysPath, "custom.pem"),
				SubPath:   "custom.pem",
			})
	}

	c.Env = append(c.Env,
		corev1.EnvVar{
			Name:  "LD_PRELOAD",
			Value: installPath + "/agent/lib64/liboneagentproc.so",
		})

	if dk.Spec.Proxy != nil && (dk.Spec.Proxy.Value != "" || dk.Spec.Proxy.ValueFrom != "") {
		c.Env = append(c.Env,
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

	if dk.Spec.NetworkZone != "" {
		c.Env = append(c.Env, corev1.EnvVar{Name: "DT_NETWORK_ZONE", Value: dk.Spec.NetworkZone})
	}

}
