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
	"context"
	"fmt"

	"go.bytebuilders.dev/license-proxyserver/apis/proxyserver"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	core "k8s.io/api/core/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func GetConfigValues(opts *ManagerOptions, kc client.Client) addonfactory.GetValuesFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *v1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
		var config core.Secret
		if err := kc.Get(context.TODO(), client.ObjectKey{Name: proxyserver.ConfigName, Namespace: proxyserver.ConfigNamespace}, &config); err != nil {
			return nil, err
		}

		var overrideValues map[string]any
		err := yaml.Unmarshal(config.Data["values.yaml"], &overrideValues)
		if err != nil {
			return nil, err
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
		if opts.Token != "" {
			err = unstructured.SetNestedField(vals, opts.Token, "platform", "token")
			if err != nil {
				return nil, err
			}
		}
		if opts.Token != "" {
			err = unstructured.SetNestedField(vals, proxyserver.HubKubeconfigSecretName, "hubKubeconfigSecretName")
			if err != nil {
				return nil, err
			}
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
						Name:      "license-proxyserver",
						Namespace: AddonInstallationNamespace,
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