package config

import (
	"net/url"
	"reflect"
	"strings"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	clientcmdapi "github.com/GoogleCloudPlatform/kubernetes/pkg/client/clientcmd/api"
	"github.com/GoogleCloudPlatform/kubernetes/third_party/golang/netutil"

	osclient "github.com/openshift/origin/pkg/client"
)

// getClusterNickname returns host:port of the clientConfig.Host, with .'s replaced by -'s
func getClusterNickname(clientCfg *client.Config) (string, error) {
	u, err := url.Parse(clientCfg.Host)
	if err != nil {
		return "", err
	}
	hostPort := netutil.CanonicalAddr(u)

	// we need a character other than "." to avoid conflicts with.  replace with '-'
	return strings.Replace(hostPort, ".", "-", -1), nil
}

// getUserNickname returns "username(as known by the server)/getClusterNickname".  This allows tab completion for switching users to
// work easily and obviously.
func getUserNickname(clientCfg *client.Config) (string, error) {
	client, err := osclient.New(clientCfg)
	if err != nil {
		return "", err
	}
	userInfo, err := client.Users().Get("~")
	if err != nil {
		return "", err
	}

	clusterNick, err := getClusterNickname(clientCfg)
	if err != nil {
		return "", err
	}

	return userInfo.Name + "/" + clusterNick, nil
}

// getContextNickname returns "namespace/getClusterNickname/username(as known by the server)".  This allows tab completion for switching projects/context
// to work easily.  First tab is the most selective on project.  Second stanza in the next most selective on cluster name.  The chances of a user trying having
// one projects on a single server that they want to operate against with two identities is low, so username is last.
func getContextNickname(namespace string, clientCfg *client.Config) (string, error) {
	client, err := osclient.New(clientCfg)
	if err != nil {
		return "", err
	}
	userInfo, err := client.Users().Get("~")
	if err != nil {
		return "", err
	}

	clusterNick, err := getClusterNickname(clientCfg)
	if err != nil {
		return "", err
	}

	return namespace + "/" + clusterNick + "/" + userInfo.Name, nil
}

// CreatePartialConfig takes a clientCfg and builds a config (kubeconfig style) from it.
func CreateConfig(namespace string, clientCfg *client.Config) (*clientcmdapi.Config, error) {
	clusterNick, err := getClusterNickname(clientCfg)
	if err != nil {
		return nil, err
	}

	userNick, err := getUserNickname(clientCfg)
	if err != nil {
		return nil, err
	}

	contextNick, err := getContextNickname(namespace, clientCfg)
	if err != nil {
		return nil, err
	}

	config := clientcmdapi.NewConfig()

	credentials := clientcmdapi.NewAuthInfo()
	credentials.Token = clientCfg.BearerToken
	credentials.ClientCertificate = clientCfg.TLSClientConfig.CertFile
	if len(credentials.ClientCertificate) == 0 {
		credentials.ClientCertificateData = clientCfg.TLSClientConfig.CertData
	}
	credentials.ClientKey = clientCfg.TLSClientConfig.KeyFile
	if len(credentials.ClientKey) == 0 {
		credentials.ClientKeyData = clientCfg.TLSClientConfig.KeyData
	}
	config.AuthInfos[userNick] = *credentials

	cluster := clientcmdapi.NewCluster()
	cluster.Server = clientCfg.Host
	cluster.CertificateAuthority = clientCfg.CAFile
	if len(cluster.CertificateAuthority) == 0 {
		cluster.CertificateAuthorityData = clientCfg.CAData
	}
	cluster.InsecureSkipTLSVerify = clientCfg.Insecure
	config.Clusters[clusterNick] = *cluster

	context := clientcmdapi.NewContext()
	context.Cluster = clusterNick
	context.AuthInfo = userNick
	context.Namespace = namespace
	config.Contexts[contextNick] = *context
	config.CurrentContext = contextNick

	return config, nil
}

// MergeConfig adds the additional Config stanzas to the startingConfig.  It blindly stomps clusters and users, but
// it searches for a matching context before writing a new one.
func MergeConfig(startingConfig, addition clientcmdapi.Config) (*clientcmdapi.Config, error) {
	ret := startingConfig

	for requestedKey, value := range addition.Clusters {
		ret.Clusters[requestedKey] = value
	}

	for requestedKey, value := range addition.AuthInfos {
		ret.AuthInfos[requestedKey] = value
	}

	requestedContextNamesToActualContextNames := map[string]string{}
	for requestedKey, newContext := range addition.Contexts {
		actualContext := clientcmdapi.NewContext()
		actualContext.AuthInfo = newContext.AuthInfo
		actualContext.Cluster = newContext.Cluster
		actualContext.Namespace = newContext.Namespace
		actualContext.Extensions = newContext.Extensions

		if existingName := FindExistingContextName(startingConfig, *actualContext); len(existingName) > 0 {
			// if this already exists, just move to the next, our job is done
			requestedContextNamesToActualContextNames[requestedKey] = existingName
			continue
		}

		requestedContextNamesToActualContextNames[requestedKey] = requestedKey
		ret.Contexts[requestedKey] = *actualContext
	}

	if len(addition.CurrentContext) > 0 {
		if newCurrentContext, exists := requestedContextNamesToActualContextNames[addition.CurrentContext]; exists {
			ret.CurrentContext = newCurrentContext
		} else {
			ret.CurrentContext = addition.CurrentContext
		}
	}

	return &ret, nil
}

// FindExistingContextName finds the nickname for the passed context
func FindExistingContextName(haystack clientcmdapi.Config, needle clientcmdapi.Context) string {
	for key, context := range haystack.Contexts {
		context.LocationOfOrigin = ""
		if reflect.DeepEqual(context, needle) {
			return key
		}
	}

	return ""
}
