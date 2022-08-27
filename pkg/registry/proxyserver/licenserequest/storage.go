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
	"fmt"
	"strings"

	proxyv1alpha1 "go.bytebuilders.dev/license-proxyserver/apis/proxyserver/v1alpha1"
	"go.bytebuilders.dev/license-proxyserver/pkg/storage"
	verifier "go.bytebuilders.dev/license-verifier"
	"go.bytebuilders.dev/license-verifier/apis/licenses/v1alpha1"
	"go.bytebuilders.dev/license-verifier/client"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
)

type Storage struct {
	cid    string
	caCert *x509.Certificate
	lc     *client.Client
	reg    *storage.LicenseRegistry
	rb     *storage.RecordBook
}

var (
	_ rest.GroupVersionKindProvider = &Storage{}
	_ rest.Scoper                   = &Storage{}
	_ rest.Creater                  = &Storage{}
)

func NewStorage(cid string, caCert *x509.Certificate, lc *client.Client, reg *storage.LicenseRegistry, rb *storage.RecordBook) *Storage {
	s := &Storage{
		cid:    cid,
		caCert: caCert,
		lc:     lc,
		reg:    reg,
		rb:     rb,
	}
	return s
}

func (r *Storage) GroupVersionKind(_ schema.GroupVersion) schema.GroupVersionKind {
	return proxyv1alpha1.SchemeGroupVersion.WithKind(proxyv1alpha1.ResourceKindLicenseRequest)
}

func (r *Storage) NamespaceScoped() bool {
	return false
}

func (r *Storage) New() runtime.Object {
	return &proxyv1alpha1.LicenseRequest{}
}

func (r *Storage) Create(ctx context.Context, obj runtime.Object, _ rest.ValidateObjectFunc, _ *metav1.CreateOptions) (runtime.Object, error) {
	user, ok := request.UserFrom(ctx)
	if !ok {
		return nil, apierrors.NewBadRequest("missing user info")
	}
	req := obj.(*proxyv1alpha1.LicenseRequest)
	fmt.Println(user.GetName())

	l, err := r.getLicense(req.Request.Features)
	if err != nil {
		return nil, err
	}

	r.rb.Record(l.ID, strings.Join(req.Request.Features, ","), user)

	req.Response = &proxyv1alpha1.LicenseRequestResponse{
		Contract: nil,
		License:  string(l.Data),
	}
	return req, nil
}

func (r *Storage) getLicense(features []string) (*v1alpha1.License, error) {
	for _, feature := range features {
		l, ok := r.reg.LicenseForFeature(feature)
		if ok {
			return l, nil
		}
	}
	nl, err := r.lc.AcquireLicense(features)
	if err != nil {
		return nil, err
	}
	l, err := verifier.ParseLicense(verifier.ParserOptions{
		ClusterUID: r.cid,
		CACert:     r.caCert,
		License:    nl,
	})
	if err != nil {
		return nil, err
	}
	r.reg.Add(l)
	return &l, nil
}
