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
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"go.bytebuilders.dev/license-verifier/apis/licenses/v1alpha1"
)

const minRemainingLife = 10 * time.Minute

// A LicenseQueue implements heap.Interface and holds Items.
type LicenseQueue []*v1alpha1.License

func (pq LicenseQueue) Len() int { return len(pq) }

func (pq LicenseQueue) Less(i, j int) bool {
	return pq[i].Less(pq[j])
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

type Record struct {
	License  *v1alpha1.License
	Contract *v1alpha1.Contract
}

type LicenseRegistry struct {
	m        sync.Mutex
	reg      map[string]LicenseQueue // feature -> heap
	store    map[string]*Record      // serial # -> Record
	rb       *RecordBook
	cacheDir string
}

func NewLicenseRegistry(cacheDir string, rb *RecordBook) *LicenseRegistry {
	return &LicenseRegistry{
		cacheDir: cacheDir,
		reg:      make(map[string]LicenseQueue),
		store:    make(map[string]*Record),
		rb:       rb,
	}
}

func (r *LicenseRegistry) Add(l *v1alpha1.License, c *v1alpha1.Contract) {
	r.m.Lock()
	defer r.m.Unlock()

	if _, ok := r.store[l.ID]; ok {
		return
	}

	r.store[l.ID] = &Record{License: l, Contract: c}
	for _, feature := range l.Features {
		q, ok := r.reg[feature]
		if !ok {
			q := make(LicenseQueue, 1)
			q[0] = l
			// heap.Init(&q) // not needed?
			r.reg[feature] = q
			continue
		}
		heap.Push(&q, l)
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
	now := time.Now().Add(minRemainingLife)
	for q.Len() > 0 {
		// ref: https://stackoverflow.com/a/63328950
		item := q[0]
		if now.After(item.NotAfter.Time) {
			heap.Pop(&q)
			r.reg[feature] = q
			r.removeFromStore(item)
		} else {
			return item, true
		}
	}
	return nil, false
}

func (r *LicenseRegistry) addToStore(l *v1alpha1.License, c *v1alpha1.Contract) {
	r.store[l.ID] = &Record{License: l, Contract: c}
	if r.cacheDir != "" {
		_ = os.WriteFile(filepath.Join(r.cacheDir, l.ID), l.Data, 0o644)
	}
}

func (r *LicenseRegistry) removeFromStore(l *v1alpha1.License) {
	delete(r.store, l.ID)
	if r.rb != nil {
		r.rb.Delete(l.ID)
	}
	if r.cacheDir != "" {
		_ = os.Remove(filepath.Join(r.cacheDir, l.ID))
	}
}

func (r *LicenseRegistry) Get(id string) (*Record, bool) {
	r.m.Lock()
	defer r.m.Unlock()

	q, ok := r.store[id]
	return q, ok
}

func (r *LicenseRegistry) List() []*Record {
	r.m.Lock()
	defer r.m.Unlock()

	now := time.Now().Add(minRemainingLife)
	out := make([]*Record, 0, len(r.store))
	for _, rec := range r.store {
		if rec.License.NotAfter.After(now) {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].License.Less(out[j].License)
	})
	return out
}
