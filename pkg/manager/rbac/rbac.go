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

package rbac

import (
	"context"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func SetupPermission(kubeConfig *rest.Config, agentName string) agent.PermissionConfigFunc {
	return func(cluster *clusterv1.ManagedCluster, addon *addonv1alpha1.ManagedClusterAddOn) error {
		nativeClient, err := kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			return err
		}
		namespace := cluster.Name
		agentUser := agent.DefaultUser(cluster.Name, addon.Name, agentName)

		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addon.Name,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "addon.open-cluster-management.io/v1alpha1",
						Kind:               "ManagedClusterAddOn",
						UID:                addon.UID,
						Name:               addon.Name,
						BlockOwnerDeletion: ptr.To(true),
					},
				},
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Verbs:     []string{"get", "list", "watch"},
					Resources: []string{"secrets"},
				},
			},
		}
		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addon.Name,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "addon.open-cluster-management.io/v1alpha1",
						Kind:               "ManagedClusterAddOn",
						UID:                addon.UID,
						Name:               addon.Name,
						BlockOwnerDeletion: ptr.To(true),
					},
				},
			},
			RoleRef: rbacv1.RoleRef{
				Kind: "Role",
				Name: addon.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: rbacv1.UserKind,
					Name: agentUser,
				},
			},
		}

		_, err = nativeClient.RbacV1().Roles(cluster.Name).Get(context.TODO(), role.Name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			_, createErr := nativeClient.RbacV1().Roles(cluster.Name).Create(context.TODO(), role, metav1.CreateOptions{})
			if createErr != nil {
				return createErr
			}
		case err != nil:
			return err
		}

		_, err = nativeClient.RbacV1().RoleBindings(cluster.Name).Get(context.TODO(), roleBinding.Name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			_, createErr := nativeClient.RbacV1().RoleBindings(cluster.Name).Create(context.TODO(), roleBinding, metav1.CreateOptions{})
			if createErr != nil {
				return createErr
			}
		case err != nil:
			return err
		}

		return nil
	}
}
