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
	"github.com/Dynatrace/dynatrace-operator/src/controllers/dynakube/oneagent/daemonset"
	"github.com/Dynatrace/dynatrace-operator/src/deploymentmetadata"
	"github.com/Dynatrace/dynatrace-operator/src/kubeobjects"
	"github.com/Dynatrace/dynatrace-operator/src/kubesystem"
	"github.com/Dynatrace/dynatrace-operator/src/mapper"
	"github.com/Dynatrace/dynatrace-operator/src/standalone"
	dtwebhook "github.com/Dynatrace/dynatrace-operator/src/webhook"
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

	mgr.GetWebhookServer().Register("/inject", &webhook.Admission{Handler: &podMutator{
		metaClient: metaClient,
		apiReader:  mgr.GetAPIReader(),
		namespace:  ns,
		image:      pod.Spec.Containers[0].Image,
		apmExists:  apmExists,
		clusterID:  string(UID),
		recorder:   mgr.GetEventRecorderFor("Webhook Server"),
	}})
	return nil
}

func registerHealthzEndpoint(mgr manager.Manager) {
	mgr.GetWebhookServer().Register("/livez", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

// podMutator injects the OneAgent into Pods
type podMutator struct {
	client         client.Client
	metaClient     client.Client
	apiReader      client.Reader
	decoder        *admission.Decoder
	image          string
	namespace      string
	apmExists      bool
	clusterID      string
	currentPodName string
	recorder       record.EventRecorder
}

// InjectClient injects the client
func (m *podMutator) InjectClient(c client.Client) error {
	m.client = c
	return nil
}

// InjectDecoder injects the decoder
func (m *podMutator) InjectDecoder(d *admission.Decoder) error {
	m.decoder = d
	return nil
}

// getResponseForPod tries to format pod as json
func getResponseForPod(pod *corev1.Pod, req *admission.Request) admission.Response {
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

func (m *podMutator) getPod(req admission.Request) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	err := m.decoder.Decode(req, pod)
	if err != nil {
		podLog.Error(err, "Failed to decode the request for pod injection")
		return nil, err
	}
	return pod, nil
}

func (m *podMutator) getNsAndDkName(ctx context.Context, req admission.Request) (corev1.Namespace, string, error) {
	var namespace corev1.Namespace

	if err := m.client.Get(ctx, client.ObjectKey{Name: req.Namespace}, &namespace); err != nil {
		podLog.Error(err, "Failed to query the namespace before pod injection")
		return corev1.Namespace{}, "", err
	}

	dkName, ok := namespace.Labels[mapper.InstanceLabel]
	if !ok {
		var err error
		if !kubesystem.DeployedViaOLM() {
			err = fmt.Errorf("no DynaKube instance set for namespace: %s", req.Namespace)
		}
		return corev1.Namespace{}, "", err
	}
	return namespace, dkName, nil
}

func (m *podMutator) getDynakube(ctx context.Context, req admission.Request, dkName string) (dynatracev1beta1.DynaKube, error) {
	var dk dynatracev1beta1.DynaKube
	if err := m.client.Get(ctx, client.ObjectKey{Name: dkName, Namespace: m.namespace}, &dk); k8serrors.IsNotFound(err) {
		template := "namespace '%s' is assigned to DynaKube instance '%s' but doesn't exist"
		m.recorder.Eventf(
			&dynatracev1beta1.DynaKube{ObjectMeta: metav1.ObjectMeta{Name: "placeholder", Namespace: m.namespace}},
			corev1.EventTypeWarning,
			missingDynakubeEvent,
			template, req.Namespace, dkName)
		return dynatracev1beta1.DynaKube{}, err
	} else if err != nil {
		return dynatracev1beta1.DynaKube{}, err
	}
	return dk, nil
}

// podMutator adds an annotation to every incoming pods
func (m *podMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	emptyPatch := admission.Patched("")

	if m.apmExists {
		return emptyPatch
	}

	pod, err := m.getPod(req)
	if err != nil {
		return silentErrorResponse(req.Name, err)
	}
	m.currentPodName = pod.Name
	defer func() {
		m.currentPodName = ""
	}()

	ns, dkName, err := m.getNsAndDkName(ctx, req)
	if err == nil && dkName == "" {
		return admission.Patched("") // TODO not nice
	} else if err != nil {
		return silentErrorResponse(pod.Name, err)
	}

	dk, err := m.getDynakube(ctx, req, dkName)
	if err != nil {
		return silentErrorResponse(pod.Name, err)
	}

	if !dk.NeedAppInjection() {
		return emptyPatch
	}

	podLog.Info("injecting into Pod", "name", pod.Name, "generatedName", pod.GenerateName, "namespace", req.Namespace)

	response := m.handleAlreadyInjectedPod(pod, dk, req)
	if response != nil {
		return *response
	}

	installContainer := createInstallInitContainerBase(image, pod, failurePolicy, basePodName, sc, dk)

	updateContainers(pod, injectionInfo, &installContainer, dk, deploymentMetadata)

	addToInitContainers(pod, installContainer)

	m.recorder.Eventf(&dk,
		corev1.EventTypeNormal,
		injectEvent,
		"Injecting the necessary info into pod %s in namespace %s", basePodName, ns.Name)

	return getResponseForPod(pod, &req)
}

func (m *podMutator) handleAlreadyInjectedPod(pod *corev1.Pod, dk dynatracev1beta1.DynaKube, req admission.Request) *admission.Response {
	// are there any injections already?
	if len(pod.Annotations[dtwebhook.AnnotationDynatraceInjected]) > 0 {
		if dk.FeatureEnableWebhookReinvocationPolicy() {
			rsp := m.applyReinvocationPolicy(pod, dk, req)
			return &rsp
		}
		rsp := admission.Patched("")
		return &rsp
	}
	return nil
}

func addToInitContainers(pod *corev1.Pod, installContainer corev1.Container) {
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, installContainer)
}



func createInstallInitContainerBase(image string, pod *corev1.Pod, failurePolicy string, basePodName string, sc *corev1.SecurityContext, dk dynatracev1beta1.DynaKube) corev1.Container {
	ic := corev1.Container{
		Name:            dtwebhook.InstallContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            []string{"init"},
		Env: []corev1.EnvVar{
			{Name: standalone.ContainerCountEnv, Value: strconv.Itoa(len(pod.Spec.Containers))},
			{Name: standalone.CanFailEnv, Value: failurePolicy},
			{Name: standalone.K8PodNameEnv, ValueFrom: kubeobjects.FieldEnvVar("metadata.name")},
			{Name: standalone.K8PodUIDEnv, ValueFrom: kubeobjects.FieldEnvVar("metadata.uid")},
			{Name: standalone.K8BasePodNameEnv, Value: basePodName},
			{Name: standalone.K8NamespaceEnv, ValueFrom: kubeobjects.FieldEnvVar("metadata.namespace")},
			{Name: standalone.K8NodeNameEnv, ValueFrom: kubeobjects.FieldEnvVar("spec.nodeName")},
		},
		SecurityContext: sc,
		VolumeMounts: []corev1.VolumeMount{
			{Name: injectionConfigVolumeName, MountPath: standalone.ConfigDirMount},
		},
		Resources: *dk.InitResources(),
	}
	return ic
}

func (m *podMutator) applyReinvocationPolicy(pod *corev1.Pod, dk dynatracev1beta1.DynaKube, req admission.Request) admission.Response {
	var needsUpdate = false
	var installContainer *corev1.Container
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]

		oaInjected := false
		for _, e := range c.Env {
			if e.Name == "LD_PRELOAD" {
				oaInjected = true
				break
			}
		}

		if !oaInjected {
			// container does not have LD_PRELOAD set
			podLog.Info("instrumenting missing container", "injectable", "oneagent", "name", c.Name)

			deploymentMetadata := deploymentmetadata.NewDeploymentMetadata(m.clusterID, daemonset.DeploymentTypeApplicationMonitoring)

			updateContainerOneAgent(c, &dk, pod, deploymentMetadata)

			if installContainer == nil {
				for j := range pod.Spec.InitContainers {
					ic := &pod.Spec.InitContainers[j]

					if ic.Name == dtwebhook.InstallContainerName {
						installContainer = ic
						break
					}
				}
			}
			updateInstallContainerOneAgent(installContainer, i+1, c.Name, c.Image)

			needsUpdate = true
		}

	}

	if needsUpdate {
		podLog.Info("updating pod with missing containers")
		m.recorder.Eventf(&dk,
			corev1.EventTypeNormal,
			updatePodEvent,
			"Updating pod %s in namespace %s with missing containers", pod.GenerateName, pod.Namespace)
		return getResponseForPod(pod, &req)
	}
	return admission.Patched("")
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
