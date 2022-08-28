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

package licensestatus

import (
	"context"

	"go.bytebuilders.dev/license-proxyserver/apis/proxyserver"
	proxyv1alpha1 "go.bytebuilders.dev/license-proxyserver/apis/proxyserver/v1alpha1"
	"go.bytebuilders.dev/license-proxyserver/pkg/storage"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/registry/rest"
)

type Storage struct {
	reg       *storage.LicenseRegistry
	rb        *storage.RecordBook
	convertor rest.TableConvertor
}

var (
	_ rest.GroupVersionKindProvider = &Storage{}
	_ rest.Scoper                   = &Storage{}
	_ rest.Getter                   = &Storage{}
	_ rest.Lister                   = &Storage{}
)

func NewStorage(reg *storage.LicenseRegistry, rb *storage.RecordBook) *Storage {
	s := &Storage{
		reg: reg,
		rb:  rb,
		convertor: NewDefaultTableConvertor(schema.GroupResource{
			Group:    proxyserver.GroupName,
			Resource: proxyv1alpha1.ResourceLicenseStatuses,
		}),
	}
	return s
}

func (r *Storage) GroupVersionKind(_ schema.GroupVersion) schema.GroupVersionKind {
	return proxyv1alpha1.SchemeGroupVersion.WithKind(proxyv1alpha1.ResourceKindLicenseStatus)
}

func (r *Storage) NamespaceScoped() bool {
	return false
}

func (r *Storage) New() runtime.Object {
	return &proxyv1alpha1.LicenseStatus{}
}

func (r *Storage) NewList() runtime.Object {
	return &proxyv1alpha1.LicenseStatusList{}
}

func (r *Storage) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
	records := r.reg.List()

	items := make([]proxyv1alpha1.LicenseStatus, 0, len(records))
	for _, rec := range records {
		item := r.toLicenseStatus(rec)
		items = append(items, item)
	}

	result := proxyv1alpha1.LicenseStatusList{
		TypeMeta: metav1.TypeMeta{},
		ListMeta: metav1.ListMeta{},
		Items:    items,
	}
	return &result, nil
}

func (r *Storage) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	rec, ok := r.reg.Get(name)
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{
			Group:    proxyserver.GroupName,
			Resource: proxyv1alpha1.ResourceLicenseStatuses,
		}, name)
	}
	out := r.toLicenseStatus(rec)
	return &out, nil
}

func (r *Storage) toLicenseStatus(rec *storage.Record) proxyv1alpha1.LicenseStatus {
	item := proxyv1alpha1.LicenseStatus{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:              rec.License.ID,
			UID:               types.UID(rec.License.ID),
			CreationTimestamp: *rec.License.NotBefore,
		},
		Spec: proxyv1alpha1.LicenseStatusSpec{},
		Status: proxyv1alpha1.LicenseStatusStatus{
			License:  *rec.License,
			Contract: rec.Contract,
		},
	}
	if spec, ok := r.rb.UsedBy(rec.License.ID); ok {
		item.Spec = *spec
	}
	return item
}

func (r *Storage) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return r.convertor.ConvertToTable(ctx, object, tableOptions)
}
