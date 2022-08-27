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
	"container/heap"
	"sort"
	"sync"
	"time"

	"go.bytebuilders.dev/license-verifier/apis/licenses/v1alpha1"
)

const minLife = 10 * time.Minute

// A LicenseQueue implements heap.Interface and holds Items.
type LicenseQueue []*v1alpha1.License

func (pq LicenseQueue) Len() int { return len(pq) }

func (pq LicenseQueue) Less(i, j int) bool {
	return pq[i].Less(*pq[j])
}

func (pq LicenseQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *LicenseQueue) Push(x any) {
	item := x.(*v1alpha1.License)
	*pq = append(*pq, item)
}

func (pq *LicenseQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*pq = old[0 : n-1]
	return item
}

type LicenseRegistry struct {
	m     sync.Mutex
	reg   map[string]LicenseQueue      // feature -> heap
	store map[string]*v1alpha1.License // serial # -> License
	rb    *RecordBook
}

func NewLicenseRegistry(rb *RecordBook) *LicenseRegistry {
	return &LicenseRegistry{
		reg:   make(map[string]LicenseQueue),
		store: make(map[string]*v1alpha1.License),
		rb:    rb,
	}
}

func (r *LicenseRegistry) Add(l v1alpha1.License) {
	r.m.Lock()
	defer r.m.Unlock()

	r.store[l.ID] = &l
	for _, feature := range l.Features {
		q, ok := r.reg[feature]
		if !ok {
			q := make(LicenseQueue, 1)
			q[0] = &l
			// heap.Init(&q) // not needed?
			r.reg[feature] = q
			continue
		}
		heap.Push(&q, &l)
		r.reg[feature] = q
	}
}

func (r *LicenseRegistry) LicenseForFeature(feature string) (*v1alpha1.License, bool) {
	r.m.Lock()
	defer r.m.Unlock()

	q, ok := r.reg[feature]
	if !ok {
		return nil, false
	}
	now := time.Now().Add(minLife)
	for q.Len() > 0 {
		// ref: https://stackoverflow.com/a/63328950
		item := q[0]
		if now.After(item.NotAfter.Time) {
			heap.Pop(&q)
			r.reg[feature] = q
			delete(r.store, item.ID)
			if r.rb != nil {
				r.rb.Delete(item.ID)
			}
		} else {
			return item, true
		}
	}
	return nil, false
}

func (r *LicenseRegistry) Get(id string) (*v1alpha1.License, bool) {
	r.m.Lock()
	defer r.m.Unlock()

	q, ok := r.store[id]
	return q, ok
}

func (r *LicenseRegistry) List() []v1alpha1.License {
	r.m.Lock()
	defer r.m.Unlock()

	now := time.Now().Add(minLife)
	out := make([]v1alpha1.License, 0, len(r.store))
	for _, rl := range r.store {
		if rl.NotAfter.After(now) {
			out = append(out, *rl)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Less(out[j])
	})
	return out
}
