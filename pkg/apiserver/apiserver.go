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

package apiserver

import (
	"context"
	"fmt"
	"os"

	"go.bytebuilders.dev/license-proxyserver/apis/proxyserver"
	proxyserverinstall "go.bytebuilders.dev/license-proxyserver/apis/proxyserver/install"
	proxyserverv1alpha1 "go.bytebuilders.dev/license-proxyserver/apis/proxyserver/v1alpha1"
	"go.bytebuilders.dev/license-proxyserver/pkg/controllers/secret"
	"go.bytebuilders.dev/license-proxyserver/pkg/registry/proxyserver/licenserequest"
	"go.bytebuilders.dev/license-proxyserver/pkg/registry/proxyserver/licensestatus"
	"go.bytebuilders.dev/license-proxyserver/pkg/storage"
	licenseclient "go.bytebuilders.dev/license-verifier/client"
	pc "go.bytebuilders.dev/license-verifier/client"
	"go.bytebuilders.dev/license-verifier/info"

	core "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2/klogr"
	cu "kmodules.xyz/client-go/client"
	clustermeta "kmodules.xyz/client-go/cluster"
	ocmcluster "open-cluster-management.io/api/cluster/v1alpha1"
	ocmoperator "open-cluster-management.io/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	// Scheme defines methods for serializing and deserializing API objects.
	Scheme = runtime.NewScheme()
	// Codecs provides methods for retrieving codecs and serializers for specific
	// versions and content types.
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	proxyserverinstall.Install(Scheme)
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(ocmcluster.Install(Scheme))
	utilruntime.Must(ocmoperator.Install(Scheme))
	utilruntime.Must(core.AddToScheme(Scheme))

	// we need to add the options to empty v1
	// TODO fix the server code to avoid this
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})

	// TODO: keep the generic API server from wanting this
	unversioned := schema.GroupVersion{Group: "", Version: "v1"}
	Scheme.AddUnversionedTypes(unversioned,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)
}

// ExtraConfig holds custom apiserver config
type ExtraConfig struct {
	ClientConfig  *restclient.Config
	BaseURL       string
	Token         string
	LicenseDir    string
	CacheDir      string
	HubKubeconfig string
}

// Config defines the config for the apiserver
type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig
	ExtraConfig   ExtraConfig
}

// LicenseProxyServer contains state for a Kubernetes cluster master/api server.
type LicenseProxyServer struct {
	GenericAPIServer *genericapiserver.GenericAPIServer
	HubManager       manager.Manager
	SpokeManager     manager.Manager
}

type completedConfig struct {
	GenericConfig genericapiserver.CompletedConfig
	ExtraConfig   *ExtraConfig
}

// CompletedConfig embeds a private pointer that cannot be instantiated outside of this package.
type CompletedConfig struct {
	*completedConfig
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (cfg *Config) Complete() CompletedConfig {
	c := completedConfig{
		cfg.GenericConfig.Complete(),
		&cfg.ExtraConfig,
	}

	c.GenericConfig.Version = &version.Info{
		Major: "1",
		Minor: "0",
	}

	return CompletedConfig{&c}
}

// New returns a new instance of LicenseProxyServer from the given config.
func (c completedConfig) New(ctx context.Context) (*LicenseProxyServer, error) {
	genericServer, err := c.GenericConfig.New("proxyserver", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	log.SetLogger(klogr.New()) // nolint:staticcheck
	setupLog := log.Log.WithName("setup")

	cfg := c.ExtraConfig.ClientConfig
	spokeManager, err := manager.New(cfg, manager.Options{
		Scheme:                 Scheme,
		Metrics:                metricsserver.Options{BindAddress: ""},
		HealthProbeBindAddress: "",
		LeaderElection:         false,
		LeaderElectionID:       "5b87adeb.proxyserver.licenses.appscode.com",
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&core.Pod{},
				},
			},
		},
		NewClient: cu.NewClient,
	})
	if err != nil {
		setupLog.Error(err, "unable to start spoke manager")
		os.Exit(1)
	}

	isSpokeCluster := clustermeta.IsOpenClusterSpoke(spokeManager.GetRESTMapper())

	cid, err := clustermeta.ClusterUID(spokeManager.GetAPIReader())
	if err != nil {
		return nil, err
	}

	caData, err := info.LoadLicenseCA()
	if err != nil {
		return nil, err
	}
	caCert, err := info.ParseCertificate(caData)
	if err != nil {
		return nil, err
	}

	var lc *pc.Client
	if !isSpokeCluster {
		if c.ExtraConfig.BaseURL == "" {
			return nil, fmt.Errorf("missing --baseURL")
		}
		if c.ExtraConfig.Token == "" {
			return nil, fmt.Errorf("missing --token")
		}
		lc, err = licenseclient.NewClient(c.ExtraConfig.BaseURL, c.ExtraConfig.Token, cid)
		if err != nil {
			return nil, err
		}
	}

	rb := storage.NewRecordBook()
	reg := storage.NewLicenseRegistry(c.ExtraConfig.CacheDir, rb)
	if c.ExtraConfig.LicenseDir != "" {
		err = storage.LoadDir(cid, c.ExtraConfig.LicenseDir, reg)
		if err != nil {
			return nil, err
		}
	}
	if c.ExtraConfig.CacheDir != "" {
		err = storage.LoadDir(cid, c.ExtraConfig.CacheDir, reg)
		if err != nil {
			return nil, err
		}
	}

	s := &LicenseProxyServer{
		GenericAPIServer: genericServer,
		SpokeManager:     spokeManager,
	}

	if isSpokeCluster {
		if c.ExtraConfig.HubKubeconfig == "" {
			return nil, fmt.Errorf("missing --hub-kubeconfig")
		}
		if c.ExtraConfig.LicenseDir == "" {
			return nil, fmt.Errorf("missing --license-dir")
		}

		// create clusterClaim ID
		err = spokeManager.Add(manager.RunnableFunc(func(ctx context.Context) error {
			claim := ocmcluster.ClusterClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "id.k8s.io",
				},
				Spec: ocmcluster.ClusterClaimSpec{
					Value: cid,
				},
			}
			err := spokeManager.GetClient().Get(context.TODO(), client.ObjectKey{Name: claim.Name}, &claim)
			if kerr.IsNotFound(err) {
				return spokeManager.GetClient().Create(context.TODO(), &claim)
			}
			return err
		}))
		if err != nil {
			setupLog.Error(err, "failed to setup id clusterclaim updater")
			os.Exit(1)
		}

		// get klusterlet
		kl := ocmoperator.Klusterlet{}
		err = spokeManager.GetAPIReader().Get(context.Background(), client.ObjectKey{Name: "klusterlet"}, &kl)
		if err != nil {
			return nil, err
		}

		// get hub kubeconfig
		hubConfig, err := clientcmd.BuildConfigFromFlags("", c.ExtraConfig.HubKubeconfig)
		if err != nil {
			setupLog.Error(err, "unable to build hub rest config")
			os.Exit(1)
		}

		s.HubManager, err = manager.New(hubConfig, manager.Options{
			Scheme:                 Scheme,
			Metrics:                metricsserver.Options{BindAddress: ""},
			HealthProbeBindAddress: "",
			LeaderElection:         false,
			LeaderElectionID:       "5b87adeb-hub.proxyserver.licenses.appscode.com",
			NewClient:              cu.NewClient,
			Cache: cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					&core.Secret{}: {
						Namespaces: map[string]cache.Config{
							kl.Spec.ClusterName: {
								FieldSelector: fields.OneTermEqualSelector("metadata.name", secret.LicenseSecret),
							},
						},
					},
				},
			},
		})
		if err != nil {
			setupLog.Error(err, "unable to start hub manager")
			os.Exit(1)
		}

		if err := (&secret.LicenseSyncer{
			HubClient:   s.HubManager.GetClient(),
			SpokeClient: spokeManager.GetClient(),
			LoadLicense: func() error {
				return storage.LoadDir(cid, c.ExtraConfig.LicenseDir, reg)
			},
		}).SetupWithManager(spokeManager); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "LicenseSyncer")
			os.Exit(1)
		}
	}

	{
		apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(proxyserver.GroupName, Scheme, metav1.ParameterCodec, Codecs)

		v1alpha1storage := map[string]rest.Storage{}
		v1alpha1storage[proxyserverv1alpha1.ResourceLicenseRequests] = licenserequest.NewStorage(cid, caCert, lc, reg, rb, spokeManager.GetClient())
		v1alpha1storage[proxyserverv1alpha1.ResourceLicenseStatuses] = licensestatus.NewStorage(reg, rb)
		apiGroupInfo.VersionedResourcesStorageMap["v1alpha1"] = v1alpha1storage

		if err := s.GenericAPIServer.InstallAPIGroup(&apiGroupInfo); err != nil {
			return nil, err
		}
	}

	return s, nil
}
