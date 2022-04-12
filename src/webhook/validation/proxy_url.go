package validation

import (
	"context"
	"net/url"

	"github.com/Dynatrace/dynatrace-operator/src/agproxysecret"
	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	errorInvalidActiveGateProxyUrl = `The DynaKube's specification has an invalid Proxy URL value set. Make sure you correctly specify the URL in your custom resource.`
	errorInvalidEvalCharacter      = `The DynaKube's specification has an invalid Proxy password value set. Make sure you don't use forbidden characters.`

	errorMissingActiveGateProxySecret = `The Proxy secret indicated by the DynaKube specification doesn't exist.`

	errorInvalidProxySecretFormat = `The Proxy secret indicated by the DynaKube specification has an invalid format. Make sure you correctly creates the secret.`

	errorInvalidProxySecretUrl           = `The Proxy secret indicated by the DynaKube specification has an invalid URL value set. Make sure you correctly specify the URL in the secret.`
	errorInvalidProxySecretEvalCharacter = `The Proxy secret indicated by the DynaKube specification has an invalid Proxy password value set. Make sure you don't use forbidden characters.`
)

func invalidActiveGateProxyUrl(dv *dynakubeValidator, dynakube *dynatracev1beta1.DynaKube) string {
	if dynakube.Spec.Proxy != nil {
		if len(dynakube.Spec.Proxy.ValueFrom) > 0 {
			var proxySecret corev1.Secret
			err := dv.clt.Get(context.TODO(), client.ObjectKey{Name: dynakube.Spec.Proxy.ValueFrom, Namespace: dynakube.Namespace}, &proxySecret)
			if k8serrors.IsNotFound(err) {
				return errorMissingActiveGateProxySecret
			} else if err != nil {
				return errors.Wrap(err, "error occurred while reading PROXY secret indicated in the Dynakube specification").Error()
			}
			proxyUrl, ok := proxySecret.Data[agproxysecret.ProxySecretKey]
			if !ok {
				return errorInvalidProxySecretFormat
			}
			return validateProxyUrl(string(proxyUrl), errorInvalidProxySecretUrl, errorInvalidProxySecretEvalCharacter)
		} else if len(dynakube.Spec.Proxy.Value) > 0 {
			return validateProxyUrl(dynakube.Spec.Proxy.Value, errorInvalidActiveGateProxyUrl, errorInvalidEvalCharacter)
		}
	}
	return ""
}

// proxyUrl is valid if
// 1) encoded
// 2) password does not contain '` characters
func validateProxyUrl(proxyUrl string, parseErrorMessage string, evalErrorMessage string) string {
	if parsedUrl, err := url.Parse(proxyUrl); err != nil {
		return parseErrorMessage
	} else {
		password, _ := parsedUrl.User.Password()
		if evalIsStringInvalid(password) {
			return evalErrorMessage
		}
	}
	return ""
}

// 'eval' command is used by entrypoint.sh:readSecret function to return its result.
// For this reason apostrophe ' and backtick ` characters has to be escaped using backslash.
// On the other hand the operator needs a pure password at the same time.
// Finally, these 2 characters are forbidden.
func evalIsStringInvalid(str string) bool {
	for _, char := range str {
		if char == '\'' || char == '`' {
			return true
		}
	}
	return false
}
