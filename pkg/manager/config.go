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

package manager

import (
	"encoding/base64"
	"fmt"
	"os"

	"go.bytebuilders.dev/license-proxyserver/pkg/common"

	"github.com/pkg/errors"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"gomodules.xyz/cert/certstore"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	agentapi "open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/yaml"
)

var scheme = runtime.NewScheme()

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = clusterv1.Install(scheme)
	_ = clusterv1alpha1.Install(scheme)
	_ = apiregistrationv1.AddToScheme(scheme)
	_ = monitoringv1.AddToScheme(scheme)
}

func GetConfigValues(opts *ManagerOptions, cs *certstore.CertStore) addonfactory.GetValuesFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *v1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
		caCrtBytes, _, err := cs.ReadBytes(common.CACertName)
		if err != nil {
			return nil, err
		}

		crtBytes, keyBytes, err := cs.ReadBytes(common.ServerCertName)
		if err != nil {
			return nil, err
		}
		overrideValues := map[string]any{
			"apiserver": map[string]any{
				"servingCerts": map[string]any{
					"generate":  false,
					"caCrt":     base64.StdEncoding.EncodeToString(caCrtBytes),
					"serverCrt": base64.StdEncoding.EncodeToString(crtBytes),
					"serverKey": base64.StdEncoding.EncodeToString(keyBytes),
				},
			},
		}

		data, err := FS.ReadFile("agent-manifests/license-proxyserver/values.yaml")
		if err != nil {
			return nil, err
		}

		var values map[string]any
		err = yaml.Unmarshal(data, &values)
		if err != nil {
			return nil, err
		}

		vals := addonfactory.MergeValues(values, overrideValues)
		if opts.RegistryFQDN != "" {
			err = unstructured.SetNestedField(vals, opts.RegistryFQDN, "registryFQDN")
			if err != nil {
				return nil, err
			}
		}
		if opts.BaseURL != "" {
			err = unstructured.SetNestedField(vals, opts.BaseURL, "platform", "baseURL")
			if err != nil {
				return nil, err
			}
		}
		if opts.CAFile != "" {
			caCert, err := os.ReadFile(opts.CAFile)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to read CA file %s", opts.CAFile)
			}
			err = unstructured.SetNestedField(vals, string(caCert), "platform", "caBundle")
			if err != nil {
				return nil, err
			}
		}
		if opts.InsecureSkipVerifyTLS {
			err = unstructured.SetNestedField(vals, opts.InsecureSkipVerifyTLS, "platform", "insecureSkipVerifyTLS")
			if err != nil {
				return nil, err
			}
		}
		if opts.Token != "" {
			err = unstructured.SetNestedField(vals, opts.Token, "platform", "token")
			if err != nil {
				return nil, err
			}
		}
		if opts.Token != "" {
			err = unstructured.SetNestedField(vals, common.HubKubeconfigSecretName, "hubKubeconfigSecretName")
			if err != nil {
				return nil, err
			}
		}
		err = unstructured.SetNestedField(vals, "Always", "imagePullPolicy")
		if err != nil {
			return nil, err
		}
		err = unstructured.SetNestedField(vals, cluster.Name, "clusterName")
		if err != nil {
			return nil, err
		}

		return vals, nil
	}
}

func agentHealthProber() *agentapi.HealthProber {
	return &agentapi.HealthProber{
		Type: agentapi.HealthProberTypeWork,
		WorkProber: &agentapi.WorkHealthProber{
			ProbeFields: []agentapi.ProbeField{
				{
					ResourceIdentifier: workv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      common.AgentName,
						Namespace: common.AddonInstallationNamespace,
					},
					ProbeRules: []workv1.FeedbackRule{
						{
							Type: workv1.WellKnownStatusType,
						},
					},
				},
			},
			HealthCheck: func(identifier workv1.ResourceIdentifier, result workv1.StatusFeedbackResult) error {
				if len(result.Values) == 0 {
					return fmt.Errorf("no values are probed for deployment %s/%s", identifier.Namespace, identifier.Name)
				}
				for _, value := range result.Values {
					if value.Name != "ReadyReplicas" {
						continue
					}

					if *value.Value.Integer >= 1 {
						return nil
					}

					return fmt.Errorf("readyReplica is %d for deployement %s/%s", *value.Value.Integer, identifier.Namespace, identifier.Name)
				}
				return fmt.Errorf("readyReplica is not probed")
			},
		},
	}
}
