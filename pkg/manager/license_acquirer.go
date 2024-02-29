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
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.bytebuilders.dev/license-proxyserver/pkg/common"
	"go.bytebuilders.dev/license-proxyserver/pkg/storage"
	verifier "go.bytebuilders.dev/license-verifier"
	"go.bytebuilders.dev/license-verifier/apis/licenses/v1alpha1"
	pc "go.bytebuilders.dev/license-verifier/client"
	"go.bytebuilders.dev/license-verifier/info"

	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type LicenseAcquirer struct {
	client.Client
	BaseURL  string
	Token    string
	CacheDir string

	mu           sync.Mutex
	LicenseCache map[string]*storage.LicenseRegistry
}

var _ reconcile.Reconciler = &LicenseAcquirer{}

// SetupWithManager sets up the controller with the Manager.
func (r *LicenseAcquirer) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.ManagedCluster{}).
		Complete(r)
}

func (r *LicenseAcquirer) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling")

	managedCluster := &clusterv1.ManagedCluster{}
	err := r.Get(ctx, request.NamespacedName, managedCluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	var cid string
	var features []string
	for _, claim := range managedCluster.Status.ClusterClaims {
		if claim.Name == common.ClusterClaimClusterID {
			cid = claim.Value
		}
		if claim.Name == common.ClusterClaimLicense {
			features = strings.Split(claim.Value, ",")
		}
	}
	if cid != "" && len(features) > 0 {
		err = r.reconcile(managedCluster.Name, cid, features)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (r *LicenseAcquirer) getLicenseRegistry(cid string) (*storage.LicenseRegistry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	reg, found := r.LicenseCache[cid]
	if found {
		return reg, nil
	}

	dir := filepath.Join(r.CacheDir, cid)
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return nil, err
	}
	reg = storage.NewLicenseRegistry(dir, nil)
	r.LicenseCache[cid] = reg
	return reg, nil
}

func (r *LicenseAcquirer) reconcile(clusterName, cid string, features []string) error {
	sec := core.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.LicenseSecret,
			Namespace: clusterName,
		},
	}
	var secretExists bool
	err := r.Get(context.TODO(), client.ObjectKey{Name: sec.Name, Namespace: sec.Namespace}, &sec)
	if err == nil {
		secretExists = true
	} else if apierrors.IsNotFound(err) {
		sec.Data = map[string][]byte{}
	} else {
		return err
	}

	reg, err := r.getLicenseRegistry(cid)
	if err != nil {
		return err
	}
	for _, feature := range features {
		l, found := reg.LicenseForFeature(feature)
		if !found {
			var c *v1alpha1.Contract
			l, c, err = r.getNewLicense(cid, features)
			if err != nil {
				return err
			}
			reg.Add(l, c)
		}
		sec.Data[l.PlanName] = l.Data
	}

	if secretExists {
		return r.Update(context.TODO(), &sec)
	} else {
		return r.Create(context.TODO(), &sec)
	}
}

func (r *LicenseAcquirer) getNewLicense(cid string, features []string) (*v1alpha1.License, *v1alpha1.Contract, error) {
	lc, err := pc.NewClient(r.BaseURL, r.Token, cid)
	if err != nil {
		return nil, nil, err
	}

	lbytes, con, err := lc.AcquireLicense(features)
	if err != nil {
		return nil, nil, err
	}

	caData, err := info.LoadLicenseCA()
	if err != nil {
		return nil, nil, err
	}
	caCert, err := info.ParseCertificate(caData)
	if err != nil {
		return nil, nil, err
	}

	l, err := verifier.ParseLicense(verifier.ParserOptions{
		ClusterUID: cid,
		CACert:     caCert,
		License:    lbytes,
	})
	if err != nil {
		return nil, nil, err
	}

	return &l, con, nil
}
