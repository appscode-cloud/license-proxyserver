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

package secretfs

import (
	"context"
	"fmt"

	"gocloud.dev/blob"
	"gomodules.xyz/blobfs"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cu "kmodules.xyz/client-go/client"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SecretFS struct {
	kc  client.Client
	key client.ObjectKey
}

var _ blobfs.Interface = &SecretFS{}

func New(kc client.Client, secret types.NamespacedName) blobfs.Interface {
	return &SecretFS{
		kc:  kc,
		key: secret,
	}
}

func (s SecretFS) WriteFile(ctx context.Context, filepath string, data []byte) error {
	secret := core.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.key.Name,
			Namespace: s.key.Namespace,
		},
	}
	_, err := cu.CreateOrPatch(ctx, s.kc, &secret, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.Secret)
		if in.Data == nil {
			in.Data = map[string][]byte{}
		}
		in.Data[filepath] = data
		return in
	})
	return err
}

func (s SecretFS) ReadFile(ctx context.Context, filepath string) ([]byte, error) {
	var obj core.Secret
	err := s.kc.Get(ctx, s.key, &obj)
	if err != nil {
		return nil, err
	}
	data, found := obj.Data[filepath]
	if !found {
		return nil, fmt.Errorf("mimssing %s in secret %s", filepath, s.key)
	}
	return data, nil
}

func (s SecretFS) DeleteFile(ctx context.Context, filepath string) error {
	var secret core.Secret
	err := s.kc.Get(ctx, s.key, &secret)
	if err != nil {
		return err
	}
	_, found := secret.Data[filepath]
	if found {
		mod := secret.DeepCopy()
		delete(mod.Data, filepath)
		patch := client.StrategicMergeFrom(&secret)
		return s.kc.Patch(ctx, mod, patch)
	}
	return err
}

func (s SecretFS) Exists(ctx context.Context, filepath string) (bool, error) {
	var obj core.Secret
	err := s.kc.Get(ctx, s.key, &obj)
	if err != nil {
		return false, err
	}
	_, found := obj.Data[filepath]
	return found, nil
}

func (s SecretFS) SignedURL(_ context.Context, _ string, _ *blob.SignedURLOptions) (string, error) {
	panic("unsupported")
}
