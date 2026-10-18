package mwcache

import (
	"sync"
	"sync/atomic"
)

// renders holds the misses in flight, so a PURGE can keep their pre-edit
// render out of the cache. It is global for the reason the backend is.
var renders = inflight{byKey: map[string]map[*render]struct{}{}}

type inflight struct {
	mu    sync.Mutex
	byKey map[string]map[*render]struct{}
}

type render struct {
	purged atomic.Bool
}

// start registers a render of key. The caller ends it with finish.
func (f *inflight) start(key string) *render {
	r := new(render)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byKey[key] == nil {
		f.byKey[key] = map[*render]struct{}{}
	}
	f.byKey[key][r] = struct{}{}
	return r
}

func (f *inflight) finish(key string, r *render) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byKey[key], r)
	if len(f.byKey[key]) == 0 {
		delete(f.byKey, key)
	}
}

// purge marks every render of key in flight, so none of them is stored.
func (f *inflight) purge(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for r := range f.byKey[key] {
		r.purged.Store(true)
	}
}
