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
	"fmt"

	"go.bytebuilders.dev/license-proxyserver/pkg/common"

	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kutil "kmodules.xyz/client-go"
	cu "kmodules.xyz/client-go/client"
	meta_util "kmodules.xyz/client-go/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type LicenseSyncer struct {
	HubClient   client.Client
	SpokeClient client.Client
}

func (r *LicenseSyncer) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling")

	// get hub cluster licenses secret
	src := core.Secret{}
	err := r.HubClient.Get(ctx, request.NamespacedName, &src)
	if err != nil {
		return reconcile.Result{}, err
	}

	// get spoke cluster license secret
	dst := core.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.LicenseSecret,
			Namespace: meta_util.PodNamespace(),
		},
	}
	kt, err := cu.CreateOrPatch(ctx, r.SpokeClient, &dst, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.Secret)
		in.Data = src.Data
		return in
	})
	if err != nil {
		return reconcile.Result{}, err
	}
	if kt != kutil.VerbUnchanged {
		logger.Info(fmt.Sprintf("%s secret %s/%s", kt, dst.Namespace, dst.Name))
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
// Manager is configured to only watch license secret
func (r *LicenseSyncer) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&core.Secret{}).
		Complete(r)
}
