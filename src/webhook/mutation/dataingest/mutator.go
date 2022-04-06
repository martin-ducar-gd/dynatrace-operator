package dataingest_mutation

func (m *podMutator) retrieveWorkload(ctx context.Context, req admission.Request, injectionInfo *InjectionInfo, pod *corev1.Pod) (string, string, *admission.Response) {
	var rsp admission.Response
	var workloadName, workloadKind string
	if injectionInfo.enabled(DataIngest) {
		var err error
		workloadName, workloadKind, err = findRootOwnerOfPod(ctx, m.metaClient, pod, req.Namespace)
		if err != nil {
			rsp = silentErrorResponse(m.currentPodName, err)
			return "", "", &rsp
		}
	}
	return workloadName, workloadKind, nil
}

func findRootOwnerOfPod(ctx context.Context, clt client.Client, pod *corev1.Pod, namespace string) (string, string, error) {
	obj := &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{
			APIVersion: pod.APIVersion,
			Kind:       pod.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: pod.ObjectMeta.Name,
			// pod.ObjectMeta.Namespace is empty yet
			Namespace:       namespace,
			OwnerReferences: pod.ObjectMeta.OwnerReferences,
		},
	}
	return findRootOwner(ctx, clt, obj)
}

func findRootOwner(ctx context.Context, clt client.Client, o *metav1.PartialObjectMetadata) (string, string, error) {
	if len(o.ObjectMeta.OwnerReferences) == 0 {
		kind := o.Kind
		if kind == "Pod" {
			kind = ""
		}
		return o.ObjectMeta.Name, kind, nil
	}

	om := o.ObjectMeta
	for _, owner := range om.OwnerReferences {
		if owner.Controller != nil && *owner.Controller && isWellKnownWorkload(owner) {
			obj := &metav1.PartialObjectMetadata{
				TypeMeta: metav1.TypeMeta{
					APIVersion: owner.APIVersion,
					Kind:       owner.Kind,
				},
			}
			if err := clt.Get(ctx, client.ObjectKey{Name: owner.Name, Namespace: om.Namespace}, obj); err != nil {
				podLog.Error(err, "failed to query the object", "apiVersion", owner.APIVersion, "kind", owner.Kind, "name", owner.Name, "namespace", om.Namespace)
				return o.ObjectMeta.Name, o.Kind, err
			}

			return findRootOwner(ctx, clt, obj)
		}
	}
	return o.ObjectMeta.Name, o.Kind, nil
}

func isWellKnownWorkload(ownerRef metav1.OwnerReference) bool {
	knownWorkloads := []metav1.TypeMeta{
		{Kind: "ReplicaSet", APIVersion: "apps/v1"},
		{Kind: "Deployment", APIVersion: "apps/v1"},
		{Kind: "ReplicationController", APIVersion: "v1"},
		{Kind: "StatefulSet", APIVersion: "apps/v1"},
		{Kind: "DaemonSet", APIVersion: "apps/v1"},
		{Kind: "Job", APIVersion: "batch/v1"},
		{Kind: "CronJob", APIVersion: "batch/v1"},
		{Kind: "DeploymentConfig", APIVersion: "apps.openshift.io/v1"},
	}

	for _, knownController := range knownWorkloads {
		if ownerRef.Kind == knownController.Kind &&
			ownerRef.APIVersion == knownController.APIVersion {
			return true
		}
	}
	return false
}

func decorateInstallContainerWithDataIngest(ic *corev1.Container, injectionInfo *InjectionInfo, workloadKind string, workloadName string) {
	if injectionInfo.enabled(DataIngest) {
		ic.Env = append(ic.Env,
			corev1.EnvVar{Name: standalone.WorkloadKindEnv, Value: workloadKind},
			corev1.EnvVar{Name: standalone.WorkloadNameEnv, Value: workloadName},
			corev1.EnvVar{Name: standalone.DataIngestInjectedEnv, Value: "true"},
		)

		ic.VolumeMounts = append(ic.VolumeMounts, corev1.VolumeMount{
			Name:      dataIngestVolumeName,
			MountPath: standalone.EnrichmentPath})
	} else {
		ic.Env = append(ic.Env,
			corev1.EnvVar{Name: standalone.DataIngestInjectedEnv, Value: "false"},
		)
	}
}

func setupDataIngestVolumes(injectionInfo *InjectionInfo, pod *corev1.Pod) {
	if !injectionInfo.enabled(DataIngest) {
		return
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes,
		corev1.Volume{
			Name: dataIngestVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		corev1.Volume{
			Name: dataIngestEndpointVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: dtingestendpoint.SecretEndpointName,
				},
			},
		},
	)
}


func (m *podMutator) ensureDataIngestSecret(ctx context.Context, ns corev1.Namespace, dkName string) error {
	endpointGenerator := dtingestendpoint.NewEndpointSecretGenerator(m.client, m.apiReader, m.namespace)

	var endpointSecret corev1.Secret
	if err := m.apiReader.Get(ctx, client.ObjectKey{Name: dtingestendpoint.SecretEndpointName, Namespace: ns.Name}, &endpointSecret); k8serrors.IsNotFound(err) {
		if _, err := endpointGenerator.GenerateForNamespace(ctx, dkName, ns.Name); err != nil {
			podLog.Error(err, "failed to create the data-ingest endpoint secret before pod injection")
			return err
		}
	} else if err != nil {
		podLog.Error(err, "failed to query the data-ingest endpoint secret before pod injection")
		return err
	}

	return nil
}


func updateContainerDataIngest(c *corev1.Container, deploymentMetadata *deploymentmetadata.DeploymentMetadata) {
	podLog.Info("updating container with missing data ingest enrichment", "containerName", c.Name)

	addMetadataIfMissing(c, deploymentMetadata)

	c.VolumeMounts = append(c.VolumeMounts,
		corev1.VolumeMount{
			Name:      dataIngestVolumeName,
			MountPath: standalone.EnrichmentPath,
		},
		corev1.VolumeMount{
			Name:      dataIngestEndpointVolumeName,
			MountPath: "/var/lib/dynatrace/enrichment/endpoint",
		},
	)
}
