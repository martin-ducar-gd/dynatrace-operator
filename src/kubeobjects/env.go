package kubeobjects

import corev1 "k8s.io/api/core/v1"

func FieldEnvVar(key string) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: key}}
}
