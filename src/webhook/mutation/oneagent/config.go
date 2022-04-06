package oneagent_mutation

import (
	"github.com/Dynatrace/dynatrace-operator/src/logger"
)

var (
	log = logger.NewDTLogger().WithName("mutation-webhook.oneagent")
)


const (
	injectEvent          = "Inject"
	updatePodEvent       = "UpdatePod"
	missingDynakubeEvent = "MissingDynakube"

	oneAgentInjectedEnvVarName   = "ONEAGENT_INJECTED"
	dynatraceMetadataEnvVarName  = "DT_DEPLOYMENT_METADATA"

	oneAgentBinVolumeName   = "oneagent-bin"
	oneAgentShareVolumeName = "oneagent-share"

	injectionConfigVolumeName = "injection-config"

	provisionedVolumeMode = "provisioned"
	installerVolumeMode   = "installer"
)
