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

package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"

	proxyv1alpha1 "go.bytebuilders.dev/license-proxyserver/apis/proxyserver/v1alpha1"
	"go.bytebuilders.dev/license-proxyserver/pkg/apiserver"
	"go.bytebuilders.dev/license-proxyserver/pkg/registry/ocm"

	"github.com/spf13/pflag"
	v "gomodules.xyz/x/version"
	v1 "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/apiserver/pkg/features"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/rest"
	clientcmd2 "k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"kmodules.xyz/client-go/cluster"
	clustermeta "kmodules.xyz/client-go/cluster"
	ou "kmodules.xyz/client-go/openapi"
	"kmodules.xyz/client-go/tools/clientcmd"
	ocmv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	ocmkl "open-cluster-management.io/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	defaultEtcdPathPrefix        = "/registry/k8s.appscode.com"
	HubKubeConfigSecretName      = "license-proxyserver-hub-kubeconfig"
	HubKubeConfigSecretNamespace = "kubeops"
)

// LicenseProxyServerOptions contains state for master/api server
type LicenseProxyServerOptions struct {
	RecommendedOptions *genericoptions.RecommendedOptions
	ExtraOptions       *ExtraOptions

	StdOut io.Writer
	StdErr io.Writer
}

// NewUIServerOptions returns a new LicenseProxyServerOptions
func NewUIServerOptions(out, errOut io.Writer) *LicenseProxyServerOptions {
	_ = feature.DefaultMutableFeatureGate.Set(fmt.Sprintf("%s=false", features.APIPriorityAndFairness))
	o := &LicenseProxyServerOptions{
		RecommendedOptions: genericoptions.NewRecommendedOptions(
			defaultEtcdPathPrefix,
			apiserver.Codecs.LegacyCodec(
				proxyv1alpha1.SchemeGroupVersion,
			),
		),
		ExtraOptions: NewExtraOptions(),
		StdOut:       out,
		StdErr:       errOut,
	}
	o.RecommendedOptions.Etcd = nil
	o.RecommendedOptions.Admission = nil
	return o
}

func (o LicenseProxyServerOptions) AddFlags(fs *pflag.FlagSet) {
	o.RecommendedOptions.AddFlags(fs)
	o.ExtraOptions.AddFlags(fs)
}

// Validate validates LicenseProxyServerOptions
func (o LicenseProxyServerOptions) Validate(args []string) error {
	var errors []error
	errors = append(errors, o.RecommendedOptions.Validate()...)
	errors = append(errors, o.ExtraOptions.Validate()...)
	return utilerrors.NewAggregate(errors)
}

// Complete fills in fields required to have valid data
func (o *LicenseProxyServerOptions) Complete() error {
	return nil
}

// Config returns config for the api server given LicenseProxyServerOptions
func (o *LicenseProxyServerOptions) Config() (*apiserver.Config, error) {
	// TODO have a "real" external address
	if err := o.RecommendedOptions.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	serverConfig := genericapiserver.NewRecommendedConfig(apiserver.Codecs)
	if err := o.RecommendedOptions.ApplyTo(serverConfig); err != nil {
		return nil, err
	}
	// Fixes https://github.com/Azure/AKS/issues/522
	clientcmd.Fix(serverConfig.ClientConfig)

	ignorePrefixes := []string{
		"/swaggerapi",
		fmt.Sprintf("/apis/%s/%s", proxyv1alpha1.SchemeGroupVersion, proxyv1alpha1.ResourceLicenseRequests),
		fmt.Sprintf("/apis/%s/%s", proxyv1alpha1.SchemeGroupVersion, proxyv1alpha1.ResourceLicenseStatuses),
	}

	serverConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(
		ou.GetDefinitions(
			proxyv1alpha1.GetOpenAPIDefinitions,
		),
		openapi.NewDefinitionNamer(apiserver.Scheme))
	serverConfig.OpenAPIConfig.Info.Title = "proxyserver"
	serverConfig.OpenAPIConfig.Info.Version = v.Version.Version
	serverConfig.OpenAPIConfig.IgnorePrefixes = ignorePrefixes

	serverConfig.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(
		ou.GetDefinitions(
			proxyv1alpha1.GetOpenAPIDefinitions,
		),
		openapi.NewDefinitionNamer(apiserver.Scheme))
	serverConfig.OpenAPIV3Config.Info.Title = "proxyserver"
	serverConfig.OpenAPIV3Config.Info.Version = v.Version.Version
	serverConfig.OpenAPIV3Config.IgnorePrefixes = ignorePrefixes

	extraConfig := apiserver.ExtraConfig{
		ClientConfig: serverConfig.ClientConfig,
	}
	if err := o.ExtraOptions.ApplyTo(&extraConfig); err != nil {
		return nil, err
	}

	config := &apiserver.Config{
		GenericConfig: serverConfig,
		ExtraConfig:   extraConfig,
	}
	return config, nil
}

// RunProxyServer starts a new LicenseProxyServer given LicenseProxyServerOptions
func (o LicenseProxyServerOptions) RunProxyServer(ctx context.Context) error {
	config, err := o.Config()
	if err != nil {
		return err
	}

	server, err := config.Complete().New(ctx)
	if err != nil {
		return err
	}

	server.GenericAPIServer.AddPostStartHookOrDie("start-proxyserver-informers", func(context genericapiserver.PostStartHookContext) error {
		config.GenericConfig.SharedInformerFactory.Start(context.StopCh)
		return nil
	})

	err = server.Manager.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return server.GenericAPIServer.PrepareRun().Run(ctx.Done())
	}))
	if err != nil {
		return err
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	hc, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return err
	}
	mapper, err := apiutil.NewDynamicRESTMapper(cfg, hc)
	if err != nil {
		return err
	}
	cl, err := client.New(cfg, client.Options{
		Scheme: apiserver.Scheme,
		Mapper: mapper,
		WarningHandler: client.WarningHandlerOptions{
			SuppressWarnings:   true,
			AllowDuplicateLogs: false,
		},
	})
	if err != nil {
		return err
	}

	cid, err := clustermeta.ClusterUID(server.Manager.GetAPIReader())
	if err != nil {
		return err
	}

	cm := cluster.DetectClusterManager(cl).String()
	if strings.Contains(cm, "OCMSpoke") {
		// create clusterClaim ID
		claim := ocmv1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "id.k8s.io",
			},
			Spec: ocmv1alpha1.ClusterClaimSpec{
				Value: cid,
			},
		}

		err := cl.Get(context.TODO(), client.ObjectKey{Name: claim.Name}, &claim)
		if err != nil && kerr.IsNotFound(err) {
			err = cl.Create(context.TODO(), &claim)
			if err != nil {
				return err
			}
		} else if err != nil {
			return nil
		}

		// get klusterlet
		kl := ocmkl.Klusterlet{}
		err = cl.Get(context.Background(), client.ObjectKey{Name: "klusterlet"}, &kl)
		if err != nil {
			return err
		}

		// get hub kubeconfig from secret
		s := v1.Secret{}
		err = cl.Get(context.Background(), client.ObjectKey{Name: HubKubeConfigSecretName, Namespace: HubKubeConfigSecretNamespace}, &s)
		if err != nil {
			return err
		}

		konfig, err := clientcmd2.NewClientConfigFromBytes(s.Data["kubeconfig"])
		if err != nil {
			return err
		}

		apiConfig, err := konfig.RawConfig()
		if err != nil {
			return err
		}

		authInfo := apiConfig.Contexts[apiConfig.CurrentContext].AuthInfo
		apiConfig.AuthInfos[authInfo] = &api.AuthInfo{
			ClientCertificateData: s.Data["tls.crt"],
			ClientKeyData:         s.Data["tls.key"],
		}

		konfig = clientcmd2.NewNonInteractiveClientConfig(apiConfig, apiConfig.CurrentContext, &clientcmd2.ConfigOverrides{}, nil)
		// hub restConfig
		cfg, err = konfig.ClientConfig()
		if err != nil {
			return err
		}

		mapper, err := apiutil.NewDynamicRESTMapper(cfg, hc)
		if err != nil {
			return err
		}
		hubClient, err := client.New(cfg, client.Options{
			Scheme: apiserver.Scheme,
			Mapper: mapper,
			WarningHandler: client.WarningHandlerOptions{
				SuppressWarnings:   true,
				AllowDuplicateLogs: false,
			},
		})
		if err != nil {
			return err
		}

		fr := &ocm.SecretReconciler{
			Client:          hubClient, // hub cluster client
			InClusterClient: server.Manager.GetClient(),
			RestConfig:      cfg, // hub restConfig
			ClusterName:     kl.Spec.ClusterName,
		}
		if err := fr.SetupWithManager(server.Manager); err != nil {
			return err
		}
	}

	setupLog := log.Log.WithName("setup")
	setupLog.Info("starting manager")
	return server.Manager.Start(ctx)
}
