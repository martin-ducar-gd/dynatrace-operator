package oneagent_mutation

import (
	dtwebhook "github.com/Dynatrace/dynatrace-operator/src/webhook"
	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"

	dtcsi "github.com/Dynatrace/dynatrace-operator/src/controllers/csi"
	csivolumes "github.com/Dynatrace/dynatrace-operator/src/controllers/csi/driver/volumes"
	appvolumes "github.com/Dynatrace/dynatrace-operator/src/controllers/csi/driver/volumes/app"

	corev1 "k8s.io/api/core/v1"
)

func (mutator *OneAgentPodMutator) addVolumes() {
	addInjectionConfigVolume(mutator.pod)
	addOneAgentVolumes(mutator.dynakube, mutator.pod)

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
