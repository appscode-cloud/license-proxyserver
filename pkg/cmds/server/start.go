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
	"os"

	proxyv1alpha1 "go.bytebuilders.dev/license-proxyserver/apis/proxyserver/v1alpha1"
	"go.bytebuilders.dev/license-proxyserver/pkg/apiserver"

	"github.com/spf13/pflag"
	v "gomodules.xyz/x/version"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	utilversion "k8s.io/component-base/version"
	ou "kmodules.xyz/client-go/openapi"
	"kmodules.xyz/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	defaultEtcdPathPrefix = "/registry/k8s.appscode.com"
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

	serverConfig.EffectiveVersion = utilversion.NewEffectiveVersion("v1.0.0")
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

// Run starts a new LicenseProxyServer given LicenseProxyServerOptions
func (o LicenseProxyServerOptions) Run(ctx context.Context) error {
	setupLog := log.Log.WithName("setup")

	config, err := o.Config()
	if err != nil {
		return err
	}

	server, err := config.Complete().New(ctx)
	if err != nil {
		return err
	}

	server.GenericAPIServer.AddPostStartHookOrDie("start-proxyserver-informers", func(context genericapiserver.PostStartHookContext) error {
		config.GenericConfig.SharedInformerFactory.Start(context.Done())
		return nil
	})

	err = server.SpokeManager.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return server.GenericAPIServer.PrepareRun().RunWithContext(ctx)
	}))
	if err != nil {
		return err
	}

	if server.HubManager != nil {
		err = server.SpokeManager.Add(manager.RunnableFunc(func(ctx context.Context) error {
			setupLog.Info("starting hub manager")
			return server.HubManager.Start(ctx)
		}))
		if err != nil {
			setupLog.Error(err, "problem running hub manager")
			os.Exit(1)
		}
	}

	setupLog.Info("starting manager")
	return server.SpokeManager.Start(ctx)
}
