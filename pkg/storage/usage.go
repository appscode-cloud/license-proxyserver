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

package storage

import (
	"sync"

	proxyserver "go.bytebuilders.dev/license-proxyserver/apis/proxyserver/v1alpha1"

	"k8s.io/apiserver/pkg/authentication/user"
)

type RecordBook struct {
	m   sync.RWMutex
	reg map[string]*proxyserver.LicenseStatusSpec // id -> usage info
}

func NewRecordBook() *RecordBook {
	return &RecordBook{
		reg: make(map[string]*proxyserver.LicenseStatusSpec),
	}
}

func (r *RecordBook) Record(id, feature string, user user.Info) {
	r.m.Lock()
	defer r.m.Unlock()

	extra := make(map[string]proxyserver.ExtraValue)
	for k, v := range user.GetExtra() {
		extra[k] = v
	}
	r.reg[id] = &proxyserver.LicenseStatusSpec{
		Feature: feature,
		User: &proxyserver.UserInfo{
			Username: user.GetName(),
			UID:      user.GetUID(),
			Groups:   user.GetGroups(),
			Extra:    extra,
		},
	}
}

func (r *RecordBook) UsedBy(id string) (*proxyserver.LicenseStatusSpec, bool) {
	r.m.RLock()
	defer r.m.RUnlock()

	out, ok := r.reg[id]
	return out, ok
}

func (r *RecordBook) Delete(id string) {
	r.m.Lock()
	defer r.m.Unlock()

	delete(r.reg, id)
}
