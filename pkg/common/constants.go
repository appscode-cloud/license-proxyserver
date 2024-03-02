/*
Copyright AppsCode Inc. and Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"os"
	"time"

	meta_util "kmodules.xyz/client-go/meta"
)

const (
	ClusterClaimClusterID   = "id.k8s.io"
	ClusterClaimLicense     = "licenses.appscode.com"
	LicenseSecret           = "license-proxyserver-licenses"
	HubKubeconfigSecretName = "license-proxyserver-hub-kubeconfig"
)

const (
	AddonName                  = "license-proxyserver"
	AgentName                  = "license-proxyserver"
	AgentManifestsDir          = "agent-manifests/license-proxyserver"
	AddonInstallationNamespace = "kubeops"
	AgentConfigSecretName      = "license-proxyserver-config"

	Duration10Yrs  = 10 * 365 * 24 * time.Hour
	CACertName     = "ca"
	ServerCertName = "tls"
)

func Namespace() string {
	ns := os.Getenv("NAMESPACE") // addon manager namespace in case of ocm-mc
	if ns != "" {
		return ns
	}
	return meta_util.PodNamespace() // host name in case of ocm-mc
}
