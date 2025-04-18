/*
Copyright AppsCode Inc. and Contributors

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

package cluster

import (
	"context"
	"net/url"
	"sort"

	"github.com/rancher/norman/clientbase"
	rancher "github.com/rancher/rancher/pkg/client/generated/management/v3"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	LabelKeyRancherFieldProjectId      = "field.cattle.io/projectId"
	LabelKeyRancherHelmProjectId       = "helm.cattle.io/projectId"
	LabelKeyRancherHelmProjectOperated = "helm.cattle.io/helm-project-operated"

	FakeRancherProjectId = "p-fake"

	RancherMonitoringNamespace    = "cattle-monitoring-system"
	RancherMonitoringPrometheus   = "rancher-monitoring-prometheus"
	RancherMonitoringAlertmanager = "rancher-monitoring-alertmanager"
)

func IsRancherManaged(mapper meta.RESTMapper) bool {
	if _, err := mapper.RESTMappings(schema.GroupKind{
		Group: "management.cattle.io",
		Kind:  "Cluster",
	}); err == nil {
		return true
	}
	return false
}

func DetectRancherProxy(cfg *rest.Config) (*clientbase.ClientOpts, bool, error) {
	err := rest.LoadTLSFiles(cfg)
	if err != nil {
		return nil, false, err
	}

	kc, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, false, err
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(kc))
	if IsRancherManaged(mapper) {
		u, err := url.Parse(cfg.Host)
		if err != nil {
			return nil, false, err
		}
		u.Path = "/v3"

		opts := clientbase.ClientOpts{
			URL:      u.String(),
			TokenKey: cfg.BearerToken,
			CACerts:  string(cfg.CAData),
			// Insecure:   true,
		}
		_, err = rancher.NewClient(&opts)
		return &opts, err == nil, err
	}
	return nil, false, nil
}

func IsInDefaultProject(kc client.Client, nsName string) (bool, error) {
	return isInProject(kc, nsName, metav1.NamespaceDefault)
}

func IsInSystemProject(kc client.Client, nsName string) (bool, error) {
	return isInProject(kc, nsName, metav1.NamespaceSystem)
}

func IsInUserProject(kc client.Client, nsName string) (bool, error) {
	isDefault, err := IsInDefaultProject(kc, nsName)
	if err != nil {
		return false, err
	}
	isSys, err := IsInSystemProject(kc, nsName)
	if err != nil {
		return false, err
	}
	return !isDefault && !isSys, nil
}

func isInProject(kc client.Client, nsName, seedNS string) (bool, error) {
	if nsName == seedNS {
		return true, nil
	}

	var ns core.Namespace
	err := kc.Get(context.TODO(), client.ObjectKey{Name: nsName}, &ns)
	if err != nil {
		return false, err
	}
	projectId, exists := ns.Labels[LabelKeyRancherFieldProjectId]
	if !exists {
		return false, nil
	}

	seedProjectId, _, err := GetProjectId(kc, seedNS)
	if err != nil {
		return false, err
	}
	return projectId == seedProjectId, nil
}

func GetDefaultProjectId(kc client.Reader) (string, bool, error) {
	return GetProjectId(kc, metav1.NamespaceDefault)
}

func GetSystemProjectId(kc client.Reader) (string, bool, error) {
	return GetProjectId(kc, metav1.NamespaceSystem)
}

func GetProjectId(kc client.Reader, nsName string) (string, bool, error) {
	var ns core.Namespace
	err := kc.Get(context.TODO(), client.ObjectKey{Name: nsName}, &ns)
	if err != nil {
		return "", false, err
	}
	projectId, found := ns.Labels[LabelKeyRancherFieldProjectId]
	return projectId, found, nil
}

func ListSiblingNamespaces(kc client.Client, nsName string) ([]core.Namespace, error) {
	projectId, found, err := GetProjectId(kc, nsName)
	if err != nil || !found {
		return nil, err
	}
	return ListProjectNamespaces(kc, projectId)
}

func AreSiblingNamespaces(kc client.Client, ns1, ns2 string) (bool, error) {
	if ns1 == ns2 {
		return true, nil
	}

	p1, found, err := GetProjectId(kc, ns1)
	if err != nil || !found {
		return false, err
	}
	p2, found, err := GetProjectId(kc, ns2)
	if err != nil || !found {
		return false, err
	}
	return p1 == p2, nil
}

func ListProjectNamespaces(kc client.Client, projectId string) ([]core.Namespace, error) {
	var list core.NamespaceList
	err := kc.List(context.TODO(), &list, client.MatchingLabels{
		LabelKeyRancherFieldProjectId: projectId,
	})
	if err != nil {
		return nil, err
	}
	namespaces := list.Items
	sort.Slice(namespaces, func(i, j int) bool {
		return namespaces[i].Name < namespaces[j].Name
	})
	return namespaces, nil
}

func Names(in []core.Namespace) (ret []string) {
	ret = make([]string, 0, len(in))
	for _, ns := range in {
		ret = append(ret, ns.Name)
	}
	return
}
