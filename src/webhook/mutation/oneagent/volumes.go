package oneagent_mutation

import (
	"fmt"
	"path/filepath"

	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	dtwebhook "github.com/Dynatrace/dynatrace-operator/src/webhook"

	dtcsi "github.com/Dynatrace/dynatrace-operator/src/controllers/csi"
	csivolumes "github.com/Dynatrace/dynatrace-operator/src/controllers/csi/driver/volumes"
	appvolumes "github.com/Dynatrace/dynatrace-operator/src/controllers/csi/driver/volumes/app"
	"github.com/Dynatrace/dynatrace-operator/src/standalone"

	corev1 "k8s.io/api/core/v1"
)

const oneAgentCustomKeysPath = "/var/lib/dynatrace/oneagent/agent/customkeys"

func (mutator *OneAgentPodMutator) addVolumes() {
	addInjectionConfigVolume(mutator.pod)
	addOneAgentVolumes(mutator.dynakube, mutator.pod)

}

func addOneAgentVolumeMounts(container *corev1.Container, installPath string) {
	container.VolumeMounts = append(container.VolumeMounts,
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
			SubPath:   fmt.Sprintf(standalone.ContainerConfFilenameTemplate, container.Name),
		})
}

func addCertVolumeMounts(container *corev1.Container) {
	container.VolumeMounts = append(container.VolumeMounts,
		corev1.VolumeMount{
			Name:      oneAgentShareVolumeName,
			MountPath: filepath.Join(oneAgentCustomKeysPath, "custom.pem"),
			SubPath:   "custom.pem",
		})
}


func addInitVolumeMounts(initContainer *corev1.Container) {
	initContainer.VolumeMounts = append(initContainer.VolumeMounts,
		corev1.VolumeMount{Name: oneAgentBinVolumeName, MountPath: standalone.BinDirMount},
		corev1.VolumeMount{Name: oneAgentShareVolumeName, MountPath: standalone.ShareDirMount},
	)
}

func addInjectionConfigVolume(pod *corev1.Pod) {
	pod.Spec.Volumes = append(pod.Spec.Volumes,
		corev1.Volume{
			Name: injectionConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: dtwebhook.SecretConfigName,
				},
			},
		},
	)
}

func addOneAgentVolumes(dynakube *dynatracev1beta1.DynaKube, pod *corev1.Pod) {
	pod.Spec.Volumes = append(pod.Spec.Volumes,
		corev1.Volume{Name: oneAgentBinVolumeName, VolumeSource: getInstallerVolumeSource(dynakube)},
		corev1.Volume{
			Name: oneAgentShareVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	)
}

func getInstallerVolumeSource(dynakube *dynatracev1beta1.DynaKube) corev1.VolumeSource {
	volumeSource := corev1.VolumeSource{}
	if dynakube.NeedsCSIDriver() {
		volumeSource.CSI = &corev1.CSIVolumeSource{
			Driver: dtcsi.DriverName,
			VolumeAttributes: map[string]string{
				csivolumes.CSIVolumeAttributeModeField:     appvolumes.Mode,
				csivolumes.CSIVolumeAttributeDynakubeField: dynakube.Name,
			},
		}
	} else {
		volumeSource.EmptyDir = &corev1.EmptyDirVolumeSource{}
	}
	return volumeSource
}
