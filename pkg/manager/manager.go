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
	"embed"
	"fmt"
	"os"

	"go.bytebuilders.dev/license-proxyserver/pkg/common"
	"go.bytebuilders.dev/license-proxyserver/pkg/manager/rbac"
	"go.bytebuilders.dev/license-proxyserver/pkg/secretfs"
	"go.bytebuilders.dev/license-proxyserver/pkg/storage"

	"github.com/spf13/cobra"
	"gomodules.xyz/cert"
	"gomodules.xyz/cert/certstore"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/version"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	cu "kmodules.xyz/client-go/client"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	cmdfactory "open-cluster-management.io/addon-framework/pkg/cmd/factory"
	"open-cluster-management.io/api/addon/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

//go:embed all:agent-manifests
var FS embed.FS

func NewRegistrationOption(kubeConfig *rest.Config, addonName, agentName string) *agent.RegistrationOption {
	return &agent.RegistrationOption{
		CSRConfigurations: agent.KubeClientSignerConfigurations(addonName, agentName),
		CSRApproveCheck:   agent.ApprovalAllCSRs,
		PermissionConfig:  rbac.SetupPermission(kubeConfig, agentName),
		AgentInstallNamespace: func(addon *v1alpha1.ManagedClusterAddOn) (string, error) {
			return common.AddonInstallationNamespace, nil
		},
	}
}

func NewManagerCommand() *cobra.Command {
	opts := NewManagerOptions()

	cmd := cmdfactory.
		NewControllerCommandConfig(common.AddonName, version.Get(), func(ctx context.Context, config *rest.Config) error {
			return runManagerController(ctx, config, opts)
		}).
		NewCommand()
	cmd.Use = "manager"
	cmd.Short = "Starts the license proxy addon manager"
	opts.AddFlags(cmd.Flags())

	return cmd
}

func runManagerController(ctx context.Context, cfg *rest.Config, opts *ManagerOptions) error {
	log.SetLogger(klogr.New()) // nolint:staticcheck

	hubManager, err := ctrl.NewManager(cfg, manager.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: ""},
		HealthProbeBindAddress: "",
		LeaderElection:         false,
		LeaderElectionID:       "5b87adeb.mager.licenses.appscode.com",
		NewClient:              cu.NewClient,
	})
	if err != nil {
		return err
	}

	agentCertSecretFS := secretfs.New(hubManager.GetClient(), types.NamespacedName{
		Name:      common.AgentConfigSecretName,
		Namespace: common.Namespace(),
	})
	cs := certstore.New(agentCertSecretFS, "", common.Duration10Yrs)
	if err := hubManager.Add(manager.RunnableFunc(func(ctx context.Context) error {
		err = cs.InitCA()
		if err != nil {
			return err
		}

		_, _, err := cs.GetServerCertPair(common.ServerCertName, cert.AltNames{
			DNSNames: []string{
				fmt.Sprintf("%s.%s", common.AgentName, common.AddonInstallationNamespace),
				fmt.Sprintf("%s.%s.svc", common.AgentName, common.AddonInstallationNamespace),
			},
		})
		return err
	})); err != nil {
		klog.Error(err, "unable to initialize cert store")
		os.Exit(1)
	}
	if err := (&LicenseAcquirer{
		Client:       hubManager.GetClient(),
		BaseURL:      opts.BaseURL,
		Token:        opts.Token,
		CacheDir:     opts.CacheDir,
		LicenseCache: map[string]*storage.LicenseRegistry{},
	}).SetupWithManager(hubManager); err != nil {
		klog.Error(err, "unable to register LicenseAcquirer")
		os.Exit(1)
	}

	registrationOption := NewRegistrationOption(cfg, common.AddonName, common.AgentName)

	addonManager, err := addonmanager.New(cfg)
	if err != nil {
		return err
	}
	agent, err := addonfactory.NewAgentAddonFactory(common.AddonName, FS, common.AgentManifestsDir).
		WithScheme(scheme).
		WithGetValuesFuncs(GetConfigValues(opts, cs)).
		WithAgentRegistrationOption(registrationOption).
		WithAgentHealthProber(agentHealthProber()).
		WithAgentInstallNamespace(func(addon *v1alpha1.ManagedClusterAddOn) (string, error) {
			return common.AddonInstallationNamespace, nil
		}).
		BuildHelmAgentAddon()
	if err != nil {
		klog.Errorf("Failed to build agent: `%v`", err)
		return err
	}

	if err = addonManager.AddAgent(agent); err != nil {
		return err
	}

	go func() {
		_ = addonManager.Start(ctx)
	}()
	return hubManager.Start(ctx)
}
