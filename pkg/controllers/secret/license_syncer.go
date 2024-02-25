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

package secret

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	meta_util "kmodules.xyz/client-go/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	LicenseSecret = "license-proxyserver-licenses"
)

type LicenseSyncer struct {
	HubClient   client.Client
	SpokeClient client.Client
	LoadLicense func() error
}

func (r *LicenseSyncer) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling")

	// get hub cluster licenses secret
	sec := v1.Secret{}
	err := r.HubClient.Get(ctx, request.NamespacedName, &sec)
	if err != nil {
		return reconcile.Result{}, err
	}

	// get spoke cluster license secret
	licenseSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LicenseSecret,
			Namespace: meta_util.PodNamespace(),
		},
	}
	err = r.SpokeClient.Get(context.Background(), client.ObjectKey{Name: licenseSecret.Name, Namespace: licenseSecret.Namespace}, licenseSecret)
	switch {
	case errors.IsNotFound(err):
		err = r.SpokeClient.Create(context.Background(), licenseSecret)
		return reconcile.Result{}, err
	case err != nil:
		return reconcile.Result{}, err
	}

	licenseSecret.Data = sec.Data
	err = r.SpokeClient.Update(context.Background(), licenseSecret)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.LoadLicense()
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
// Manager is configured to only watch license secret
func (r *LicenseSyncer) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Secret{}).
		Complete(r)
}
