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

package ocm

import (
	"context"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	LicenseSecret          = "license-proxyserver-licenses"
	LicenseSecretNamespace = "kubeops"
)

type SecretReconciler struct {
	client.Client
	InClusterClient client.Client
	RestConfig      *rest.Config
	ClusterName     string
}

func (r *SecretReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling")

	// get hub cluster licenses secret
	sec := v1.Secret{}
	err := r.Client.Get(ctx, request.NamespacedName, &sec)
	if err != nil {
		return reconcile.Result{}, err
	}

	// get spoke cluster license secret
	spokeSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LicenseSecret,
			Namespace: LicenseSecretNamespace,
		},
	}
	err = r.InClusterClient.Get(context.Background(), client.ObjectKey{Name: spokeSecret.Name, Namespace: spokeSecret.Namespace}, spokeSecret)
	switch {
	case errors.IsNotFound(err):
		err = r.InClusterClient.Create(context.Background(), spokeSecret)
		return reconcile.Result{}, err
	case err != nil:
		return reconcile.Result{}, err
	}

	// check hubSecret and spokeSecret
	if reflect.DeepEqual(sec.Data, spokeSecret.Data) {
		return reconcile.Result{}, err
	}

	spokeSecret.Data = sec.Data
	err = r.InClusterClient.Update(context.Background(), spokeSecret)
	if err != nil {
		return reconcile.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Secret{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			return obj.GetName() == LicenseSecret
		}))).
		Watches(
			&source.Kind{Type: &v1.Secret{}},
			handler.EnqueueRequestsFromMapFunc(r.findSecrets()),
		).
		Complete(r)
}

func (r *SecretReconciler) findSecrets() handler.MapFunc {
	return func(object client.Object) []reconcile.Request {
		req := make([]reconcile.Request, 0)
		req = append(req, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      LicenseSecret,
				Namespace: r.ClusterName,
			},
		})

		return req
	}
}
