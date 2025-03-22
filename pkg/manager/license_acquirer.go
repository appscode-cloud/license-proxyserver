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
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.bytebuilders.dev/license-proxyserver/pkg/common"
	"go.bytebuilders.dev/license-proxyserver/pkg/storage"
	verifier "go.bytebuilders.dev/license-verifier"
	"go.bytebuilders.dev/license-verifier/apis/licenses/v1alpha1"
	pc "go.bytebuilders.dev/license-verifier/client"
	"go.bytebuilders.dev/license-verifier/info"

	v "gomodules.xyz/x/version"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const ttl = storage.LicenseAcquisitionBuffer + storage.MinRemainingLife

type LicenseAcquirer struct {
	client.Client
	BaseURL               string
	Token                 string
	CaCert                []byte
	InsecureSkipTLSVerify bool
	CacheDir              string

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
	managedCluster := &clusterv1.ManagedCluster{}
	err := r.Get(ctx, request.NamespacedName, managedCluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	var cid string
	var features []string
	for _, claim := range managedCluster.Status.ClusterClaims {
		switch claim.Name {
		case common.ClusterClaimClusterID:
			cid = claim.Value
		case common.ClusterClaimLicense:
			features = strings.Split(claim.Value, ",")
		}
	}
	if cid != "" && len(features) > 0 {
		return r.reconcile(managedCluster.Name, cid, features)
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
	reg = storage.NewLicenseRegistry(dir, ttl, nil)
	r.LicenseCache[cid] = reg
	return reg, nil
}

func (r *LicenseAcquirer) reconcile(clusterName, cid string, features []string) (reconcile.Result, error) {
	klog.InfoS("refreshing license", "clusterName", clusterName, "clusterUID", cid)

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
		return reconcile.Result{}, err
	}

	var errList []error
	var earliestExpired time.Time

	reg, err := r.getLicenseRegistry(cid)
	if err != nil {
		return reconcile.Result{}, err
	}
	for _, feature := range features {
		l, found := reg.LicenseForFeature(feature)
		if !found {
			var c *v1alpha1.Contract
			l, c, err = r.getNewLicense(cid, []string{feature})
			if err == nil {

				klog.InfoS("acquired new license",
					"clusterName", clusterName,
					"clusterUID", cid,
					"licenseID", l.ID,
					"product", l.ProductLine,
					"plan", l.PlanName,
					"expiry", l.NotAfter.UTC().Format(time.RFC822),
				)
				reg.Add(l, c)
			} else {
				klog.ErrorS(err, "failed to get new license", "feature", feature)
				var ce *x509.CertificateInvalidError
				if !errors.As(err, &ce) {
					errList = append(errList, err)
				}
			}
		}
		if l != nil && l.Status == v1alpha1.LicenseActive {
			sec.Data[l.PlanName] = l.Data
			if earliestExpired.IsZero() || earliestExpired.After(l.NotAfter.Time) {
				earliestExpired = l.NotAfter.Time
			}
		}
	}

	if secretExists {
		errList = append(errList, r.Update(context.TODO(), &sec))
	} else {
		errList = append(errList, r.Create(context.TODO(), &sec))
	}

	if !earliestExpired.IsZero() {
		return reconcile.Result{
			RequeueAfter: time.Until(earliestExpired.Add(-ttl)),
		}, utilerrors.NewAggregate(errList)
	}
	return reconcile.Result{}, utilerrors.NewAggregate(errList)
}

func (r *LicenseAcquirer) getNewLicense(cid string, features []string) (*v1alpha1.License, *v1alpha1.Contract, error) {
	lc, err := pc.NewClient(r.BaseURL, r.Token, cid, r.CaCert, r.InsecureSkipTLSVerify, fmt.Sprintf("license-proxyserver-manager/%s", v.Version.Version))
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
