package webhook

import (
	"context"

	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

type PodMutator interface {
	Enabled(pod *corev1.Pod) bool
	PodInjected(pod *corev1.Pod) bool
	ContainerInjected(container *corev1.Container) bool
	Mutate(request MutationRequest) error
	Reinvoke(request ReinvocationRequest)
}

type MutationRequest struct {
	Context       context.Context
	Pod           *corev1.Pod
	Namespace     *corev1.Namespace
	DynaKube      *dynatracev1beta1.DynaKube
	InitContainer *corev1.Container
}

type ReinvocationRequest struct {
	Pod                   *corev1.Pod
	DynaKube              *dynatracev1beta1.DynaKube
	InitContainer         *corev1.Container
	CurrentContainerIndex int
}
