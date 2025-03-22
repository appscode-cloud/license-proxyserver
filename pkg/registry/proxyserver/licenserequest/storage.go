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

package licenserequest

import (
	"context"
	"crypto/x509"
	"sort"
	"strings"

	proxyv1alpha1 "go.bytebuilders.dev/license-proxyserver/apis/proxyserver/v1alpha1"
	"go.bytebuilders.dev/license-proxyserver/pkg/common"
	"go.bytebuilders.dev/license-proxyserver/pkg/storage"
	verifier "go.bytebuilders.dev/license-verifier"
	"go.bytebuilders.dev/license-verifier/apis/licenses/v1alpha1"
	pc "go.bytebuilders.dev/license-verifier/client"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	clustermeta "kmodules.xyz/client-go/cluster"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Storage struct {
	cid         string
	caCert      *x509.Certificate
	lc          *pc.Client
	reg         *storage.LicenseRegistry
	rb          *storage.RecordBook
	spokeClient client.Client
}

var (
	_ rest.GroupVersionKindProvider = &Storage{}
	_ rest.Scoper                   = &Storage{}
	_ rest.Creater                  = &Storage{}
	_ rest.Storage                  = &Storage{}
	_ rest.SingularNameProvider     = &Storage{}
)

func NewStorage(cid string, caCert *x509.Certificate, lc *pc.Client, reg *storage.LicenseRegistry, rb *storage.RecordBook, spokeClient client.Client) *Storage {
	s := &Storage{
		cid:         cid,
		caCert:      caCert,
		lc:          lc,
		reg:         reg,
		rb:          rb,
		spokeClient: spokeClient,
	}
	return s
}

func (r *Storage) GroupVersionKind(_ schema.GroupVersion) schema.GroupVersionKind {
	return proxyv1alpha1.SchemeGroupVersion.WithKind(proxyv1alpha1.ResourceKindLicenseRequest)
}

func (r *Storage) NamespaceScoped() bool {
	return false
}

func (r *Storage) GetSingularName() string {
	return strings.ToLower(proxyv1alpha1.ResourceKindLicenseRequest)
}

func (r *Storage) New() runtime.Object {
	return &proxyv1alpha1.LicenseRequest{}
}

func (r *Storage) Create(ctx context.Context, obj runtime.Object, _ rest.ValidateObjectFunc, _ *metav1.CreateOptions) (runtime.Object, error) {
	user, ok := request.UserFrom(ctx)
	if !ok {
		return nil, apierrors.NewBadRequest("missing user info")
	}
	in := obj.(*proxyv1alpha1.LicenseRequest)

	isSpokeCluster := clustermeta.IsOpenClusterSpoke(r.spokeClient)

	l, err := r.getLicense(in.Request.Features)
	if err != nil {
		return nil, err
	} else if l == nil && isSpokeCluster {
		ca := clusterv1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: common.ClusterClaimLicense,
			},
		}
		err = r.spokeClient.Get(context.TODO(), client.ObjectKey{Name: ca.Name}, &ca)
		if err == nil {
			curFeatures := sets.New[string](strings.Split(ca.Spec.Value, ",")...)
			reqFeatures := sets.New[string](in.Request.Features...)

			extraFeatures := reqFeatures.Difference(curFeatures)
			if extraFeatures.Len() > 0 {
				curFeatures.Insert(extraFeatures.UnsortedList()...)

				ca.Spec.Value = strings.Join(sets.List[string](curFeatures), ",")
				err = r.spokeClient.Update(context.TODO(), &ca)
				if err != nil {
					return nil, err
				}
			}
		} else if apierrors.IsNotFound(err) {
			reqFeatures := in.Request.Features
			sort.Strings(reqFeatures)
			ca.Spec.Value = strings.Join(reqFeatures, ",")
			err = r.spokeClient.Create(context.TODO(), &ca)
			if err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}

		// return blank response instead of error
		in.Response = &proxyv1alpha1.LicenseRequestResponse{}
		return in, nil
	}

	if l != nil {
		r.rb.Record(l.ID, in.Request.Features, user)
		in.Response = &proxyv1alpha1.LicenseRequestResponse{
			License: string(l.Data),
		}
	} else {
		// return blank response instead of error
		// typically license mounted via secret has expired
		in.Response = &proxyv1alpha1.LicenseRequestResponse{}
	}

	return in, nil
}

func (r *Storage) getLicense(features []string) (*v1alpha1.License, error) {
	for _, feature := range features {
		l, ok := r.reg.LicenseForFeature(feature)
		if ok {
			return l, nil
		}
	}
	if r.lc == nil {
		return nil, nil
	}

	lbytes, c, err := r.lc.AcquireLicense(features)
	if err != nil {
		return nil, err
	}
	l, err := verifier.ParseLicense(verifier.ParserOptions{
		ClusterUID: r.cid,
		CACert:     r.caCert,
		License:    lbytes,
	})
	if err != nil {
		return nil, err
	}
	r.reg.Add(&l, c)
	return &l, nil
}

func (r *Storage) Destroy() {}
