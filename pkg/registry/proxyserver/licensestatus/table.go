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
	"fmt"
	"net/http"
	"time"

	"go.bytebuilders.dev/license-proxyserver/apis/proxyserver/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
)

// Adapted from: https://github.com/kubernetes/apiserver/blob/master/pkg/registry/rest/table.go

type defaultTableConvertor struct {
	defaultQualifiedResource schema.GroupResource
}

// NewDefaultTableConvertor creates a default convertor; the provided resource is used for error messages
// if no resource info can be determined from the context passed to ConvertToTable.
func NewDefaultTableConvertor(defaultQualifiedResource schema.GroupResource) rest.TableConvertor {
	return defaultTableConvertor{defaultQualifiedResource: defaultQualifiedResource}
}

var swaggerMetadataDescriptions = metav1.ObjectMeta{}.SwaggerDoc()

func (c defaultTableConvertor) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	var table metav1.Table
	fn := func(obj runtime.Object) error {
		var (
			productLine string
			username    string
			expiresAt   string
		)
		if o, ok := obj.(*v1alpha1.LicenseStatus); ok {
			productLine = o.Status.License.ProductLine
			if o.Spec.User != nil {
				username = o.Spec.User.Username
			}
			if o.Status.License.NotAfter != nil {
				expiresAt = o.Status.License.NotAfter.Time.UTC().Format(time.RFC3339)
			}
		}

		m, err := meta.Accessor(obj)
		if err != nil {
			resource := c.defaultQualifiedResource
			if info, ok := genericapirequest.RequestInfoFrom(ctx); ok {
				resource = schema.GroupResource{Group: info.APIGroup, Resource: info.Resource}
			}
			return errNotAcceptable{resource: resource}
		}
		table.Rows = append(table.Rows, metav1.TableRow{
			Cells: []interface{}{
				m.GetName(),
				productLine,
				username,
				expiresAt,
				m.GetCreationTimestamp().Time.UTC().Format(time.RFC3339),
			},
			Object: runtime.RawExtension{Object: obj},
		})
		return nil
	}
	switch {
	case meta.IsListType(object):
		if err := meta.EachListItem(object, fn); err != nil {
			return nil, err
		}
	default:
		if err := fn(object); err != nil {
			return nil, err
		}
	}
	if m, err := meta.ListAccessor(object); err == nil {
		table.ResourceVersion = m.GetResourceVersion()
		table.Continue = m.GetContinue()
		table.RemainingItemCount = m.GetRemainingItemCount()
	} else {
		if m, err := meta.CommonAccessor(object); err == nil {
			table.ResourceVersion = m.GetResourceVersion()
		}
	}
	if opt, ok := tableOptions.(*metav1.TableOptions); !ok || !opt.NoHeaders {
		table.ColumnDefinitions = []metav1.TableColumnDefinition{
			{Name: "Id", Type: "string", Format: "name", Description: swaggerMetadataDescriptions["name"]},
			{Name: "Product", Type: "string", Description: ""},
			{Name: "User", Type: "string", Description: ""},
			{Name: "Expires At", Type: "date", Description: ""},
			{Name: "Created At", Type: "date", Description: swaggerMetadataDescriptions["creationTimestamp"]},
		}
	}
	return &table, nil
}

/*
https://github.com/kubernetes/kubernetes/blob/759785ea147bc13945d521eaba4a6592cbc0675f/pkg/printers/internalversion/printers.go#L193

	{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
	{Name: "Type", Type: "string", Description: apiv1.ServiceSpec{}.SwaggerDoc()["type"]},
	{Name: "Cluster-IP", Type: "string", Description: apiv1.ServiceSpec{}.SwaggerDoc()["clusterIP"]},
	{Name: "External-IP", Type: "string", Description: apiv1.ServiceSpec{}.SwaggerDoc()["externalIPs"]},
	{Name: "Port(s)", Type: "string", Description: apiv1.ServiceSpec{}.SwaggerDoc()["ports"]},
	{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	{Name: "Selector", Type: "string", Priority: 1, Description: apiv1.ServiceSpec{}.SwaggerDoc()["selector"]},
*/

// errNotAcceptable indicates the resource doesn't support Table conversion
type errNotAcceptable struct {
	resource schema.GroupResource
}

func (e errNotAcceptable) Error() string {
	return fmt.Sprintf("the resource %s does not support being converted to a Table", e.resource)
}

func (e errNotAcceptable) Status() metav1.Status {
	return metav1.Status{
		Status:  metav1.StatusFailure,
		Code:    http.StatusNotAcceptable,
		Reason:  metav1.StatusReason("NotAcceptable"),
		Message: e.Error(),
	}
}
