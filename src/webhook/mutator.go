package webhook

import (
	"context"

	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

type PodMutator interface {
	Enabled(pod *corev1.Pod) bool
	Mutate(request MutationRequest) error
}

type MutationRequest struct {
	Context context.Context
	DynaKube *dynatracev1beta1.DynaKube
	Pod *corev1.Pod
	InitContainer *corev1.Container
	Namespace *corev1.Namespace
}
