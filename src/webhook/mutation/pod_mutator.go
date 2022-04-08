package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	"github.com/Dynatrace/dynatrace-operator/src/kubeobjects"
	"github.com/Dynatrace/dynatrace-operator/src/kubesystem"
	"github.com/Dynatrace/dynatrace-operator/src/mapper"
	"github.com/Dynatrace/dynatrace-operator/src/standalone"
	dtwebhook "github.com/Dynatrace/dynatrace-operator/src/webhook"
	oneagentmutation "github.com/Dynatrace/dynatrace-operator/src/webhook/mutation/oneagent"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const oneAgentCustomKeysPath = "/var/lib/dynatrace/oneagent/agent/customkeys"

var podLog = log.WithName("pod")

// AddPodMutationWebhookToManager adds the Webhook server to the Manager
func AddPodMutationWebhookToManager(mgr manager.Manager, ns string) error {
	podName := os.Getenv("POD_NAME")
	if podName == "" {
		podLog.Info("no Pod name set for webhook container")
	}

	if err := registerInjectEndpoint(mgr, ns, podName); err != nil {
		return err
	}
	registerHealthzEndpoint(mgr)
	return nil
}

func registerInjectEndpoint(mgr manager.Manager, ns string, podName string) error {
	// Don't use mgr.GetClient() on this function, or other cache-dependent functions from the manager. The cache may
	// not be ready at this point, and queries for Kubernetes objects may fail. mgr.GetAPIReader() doesn't depend on the
	// cache and is safe to use.

	apmExists, err := kubeobjects.CheckIfOneAgentAPMExists(mgr.GetConfig())
	if err != nil {
		return err
	}
	if apmExists {
		podLog.Info("OneAgentAPM object detected - DynaKube webhook won't inject until the OneAgent Operator has been uninstalled")
	}

	var pod corev1.Pod
	if err := mgr.GetAPIReader().Get(context.TODO(), client.ObjectKey{
		Name:      podName,
		Namespace: ns,
	}, &pod); err != nil {
		return err
	}

	var UID types.UID
	if UID, err = kubesystem.GetUID(mgr.GetAPIReader()); err != nil {
		return err
	}

	// the injected podMutator.client doesn't have permissions to Get(sth) from a different namespace
	metaClient, err := client.New(mgr.GetConfig(), client.Options{})
	if err != nil {
		return err
	}

	mgr.GetWebhookServer().Register("/inject", &webhook.Admission{Handler: &podMutatorWebhook{
		metaClient: metaClient,
		apiReader:  mgr.GetAPIReader(),
		namespace:  ns,
		image:      pod.Spec.Containers[0].Image,
		apmExists:  apmExists,
		clusterID:  string(UID),
		recorder:   mgr.GetEventRecorderFor("Webhook Server"),
		mutators: []dtwebhook.PodMutator{
			oneagentmutation.NewOneAgentPodMutator(
				pod.Spec.Containers[0].Image,
				string(UID),
				mgr.GetClient(),
				mgr.GetAPIReader(),
			),
		},
	}})
	return nil
}

func registerHealthzEndpoint(mgr manager.Manager) {
	mgr.GetWebhookServer().Register("/livez", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

// podMutatorWebhook injects the OneAgent into Pods
type podMutatorWebhook struct {
	client     client.Client
	metaClient client.Client
	apiReader  client.Reader
	decoder    *admission.Decoder
	image      string
	namespace  string
	apmExists  bool
	clusterID  string
	mutators   []dtwebhook.PodMutator
	recorder   record.EventRecorder
}

// InjectClient injects the client
func (webhook *podMutatorWebhook) InjectClient(c client.Client) error {
	webhook.client = c
	return nil
}

// InjectDecoder injects the decoder
func (webhook *podMutatorWebhook) InjectDecoder(d *admission.Decoder) error {
	webhook.decoder = d
	return nil
}

func (webhook *podMutatorWebhook) getPod(req admission.Request) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	err := webhook.decoder.Decode(req, pod)
	if err != nil {
		podLog.Error(err, "Failed to decode the request for pod injection")
		return nil, err
	}
	return pod, nil
}

func (webhook *podMutatorWebhook) getRequestNamespace(ctx context.Context, req admission.Request) (corev1.Namespace, error) {
	var namespace corev1.Namespace

	if err := webhook.client.Get(ctx, client.ObjectKey{Name: req.Namespace}, &namespace); err != nil {
		podLog.Error(err, "Failed to query the namespace before pod injection")
		return corev1.Namespace{}, err
	}

	return namespace, nil
}

func (webhook *podMutatorWebhook) getDynakubeName(namespace *corev1.Namespace) (string, error) {
	dynakubeName, ok := namespace.Labels[mapper.InstanceLabel]
	if !ok {
		var err error
		if !kubesystem.DeployedViaOLM() {
			err = fmt.Errorf("no DynaKube instance set for namespace: %s", namespace.Name)
		}
		return dynakubeName, err
	}
	return dynakubeName, nil
}

func (webhook *podMutatorWebhook) getDynakube(ctx context.Context, namespace *corev1.Namespace, dynakubeName string) (dynatracev1beta1.DynaKube, error) {
	var dk dynatracev1beta1.DynaKube
	if err := webhook.client.Get(ctx, client.ObjectKey{Name: dynakubeName, Namespace: webhook.namespace}, &dk); k8serrors.IsNotFound(err) {
		template := "namespace '%s' is assigned to DynaKube instance '%s' but doesn't exist"
		webhook.recorder.Eventf(
			&dynatracev1beta1.DynaKube{ObjectMeta: metav1.ObjectMeta{Name: "placeholder", Namespace: webhook.namespace}},
			corev1.EventTypeWarning,
			missingDynakubeEvent,
			template, namespace.Name, dynakubeName)
		return dynatracev1beta1.DynaKube{}, err
	} else if err != nil {
		return dynatracev1beta1.DynaKube{}, err
	}
	return dk, nil
}

// podMutator adds an annotation to every incoming pods
func (webhook *podMutatorWebhook) Handle(ctx context.Context, request admission.Request) admission.Response {
	emptyPatch := admission.Patched("")

	if webhook.apmExists {
		return emptyPatch
	}

	pod, err := webhook.getPod(request)
	if err != nil {
		return silentErrorResponse(request.Name, err)
	}

	namespace, err := webhook.getRequestNamespace(ctx, request)
	if err != nil {
		return silentErrorResponse(pod.Name, err)
	}

	dynakubeName, err := webhook.getDynakubeName(&namespace)
	if err == nil && dynakubeName == "" {
		return admission.Patched("") // TODO not nice
	} else if err != nil {
		return silentErrorResponse(pod.Name, err)
	}

	dynakube, err := webhook.getDynakube(ctx, &namespace, dynakubeName)
	if err != nil {
		return silentErrorResponse(pod.Name, err)
	}

	if !dynakube.NeedAppInjection() {
		return emptyPatch
	}

	podLog.Info("injecting into Pod", "name", pod.Name, "generatedName", pod.GenerateName, "namespace", request.Namespace)

	response := webhook.handleAlreadyInjectedPod(request, pod, dynakube)
	if response != nil {
		return *response
	}

	initContainer := webhook.createInstallInitContainerBase(pod, dynakube)

	mutationRequest := dtwebhook.MutationRequest{
		Context:       ctx,
		Pod:           pod,
		DynaKube:      &dynakube,
		InitContainer: &initContainer,
		Namespace:     &namespace,
	}

	for _, mutator := range webhook.mutators {
		if !mutator.Enabled(pod) {
			continue
		}
		if err := mutator.Mutate(mutationRequest); err != nil {
			return silentErrorResponse(pod.Name, err)
		}
	}

	addToInitContainers(pod, initContainer)

	webhook.recorder.Eventf(&dynakube,
		corev1.EventTypeNormal,
		injectEvent,
		"Injecting the necessary info into pod %s in namespace %s", pod.Name, &namespace.Name)

	return createResponseForPod(pod, &request)
}

func (webhook *podMutatorWebhook) createInstallInitContainerBase(pod *corev1.Pod, dk dynatracev1beta1.DynaKube) corev1.Container {
	ic := corev1.Container{
		Name:            dtwebhook.InstallContainerName,
		Image:           webhook.image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            []string{"init"},
		Env: []corev1.EnvVar{
			{Name: standalone.ContainerCountEnv, Value: strconv.Itoa(len(pod.Spec.Containers))},
			{Name: standalone.CanFailEnv, Value: kubeobjects.GetField(pod.Annotations, dtwebhook.AnnotationFailurePolicy, "silent")},
			{Name: standalone.K8PodNameEnv, ValueFrom: kubeobjects.FieldEnvVar("metadata.name")},
			{Name: standalone.K8PodUIDEnv, ValueFrom: kubeobjects.FieldEnvVar("metadata.uid")},
			{Name: standalone.K8BasePodNameEnv, Value: getBasePodName(pod)},
			{Name: standalone.K8NamespaceEnv, ValueFrom: kubeobjects.FieldEnvVar("metadata.namespace")},
			{Name: standalone.K8NodeNameEnv, ValueFrom: kubeobjects.FieldEnvVar("spec.nodeName")},
		},
		SecurityContext: getSecurityContext(pod),
		VolumeMounts: []corev1.VolumeMount{
			{Name: injectionConfigVolumeName, MountPath: standalone.ConfigDirMount},
		},
		Resources: *dk.InitResources(),
	}
	return ic
}

func (webhook *podMutatorWebhook) handleAlreadyInjectedPod(
	request admission.Request,
	pod *corev1.Pod,
	dynakube dynatracev1beta1.DynaKube) *admission.Response {
	if dynakube.FeatureEnableWebhookReinvocationPolicy() {
		if webhook.applyReinvocationPolicy(pod, dynakube) {
			podLog.Info("updating pod with missing containers")
			webhook.recorder.Eventf(&dynakube,
				corev1.EventTypeNormal,
				updatePodEvent,
				"Updating pod %s in namespace %s with missing containers", pod.GenerateName, pod.Namespace)
			response := createResponseForPod(pod, &request)
			return &response
		}
	}
	response := admission.Patched("")
	return &response
}

func (webhook *podMutatorWebhook) applyReinvocationPolicy(pod *corev1.Pod, dynakube dynatracev1beta1.DynaKube) bool {
	var needsUpdate = false
	var initContainer *corev1.Container
	for i := range pod.Spec.Containers {
		currentContainer := &pod.Spec.Containers[i]

		if initContainer == nil {
			initContainer = findInitContainer(pod.Spec.InitContainers)
		}

		for _, mutator := range webhook.mutators {
			if !mutator.PodInjected(pod) || mutator.ContainerInjected(currentContainer) {
				continue
			}
			needsUpdate = true
			mutator.Reinvoke(
				dtwebhook.ReinvocationRequest{
					Pod:              pod,
					DynaKube:         &dynakube,
					InitContainer:    initContainer,
					CurrentContainerIndex: i,
				},
			)
		}
	}
	return needsUpdate
}

func findInitContainer(initContainers []corev1.Container) *corev1.Container {
	for _, container := range initContainers {
		if container.Name == dtwebhook.InstallContainerName {
			return &container
		}
	}
	return nil
}

func getSecurityContext(pod *corev1.Pod) *corev1.SecurityContext {
	var sc *corev1.SecurityContext
	if pod.Spec.Containers[0].SecurityContext != nil {
		sc = pod.Spec.Containers[0].SecurityContext.DeepCopy()
	}
	return sc
}

func getBasePodName(pod *corev1.Pod) string {
	basePodName := pod.GenerateName
	if basePodName == "" {
		basePodName = pod.Name
	}

	// Only include up to the last dash character, exclusive.
	if p := strings.LastIndex(basePodName, "-"); p != -1 {
		basePodName = basePodName[:p]
	}
	return basePodName
}

func addToInitContainers(pod *corev1.Pod, installContainer corev1.Container) {
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, installContainer)
}

// createResponseForPod tries to format pod as json
func createResponseForPod(pod *corev1.Pod, req *admission.Request) admission.Response {
	marshaledPod, err := json.MarshalIndent(pod, "", "  ")
	if err != nil {
		return silentErrorResponse(pod.Name, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func silentErrorResponse(podName string, err error) admission.Response {
	rsp := admission.Patched("")
	rsp.Result.Message = fmt.Sprintf("Failed to inject into pod: %s because %s", podName, err.Error())
	return rsp
}
