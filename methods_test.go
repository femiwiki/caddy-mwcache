package mwcache

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// HEAD shares the cache with GET.
func TestHeadIsServedFromCache(t *testing.T) {
	h, _ := newTestHandler()
	next := cacheableUpstream()

	serve(t, h, next, httptest.NewRequest(http.MethodGet, "/w/Article", nil))
	serve(t, h, next, httptest.NewRequest(http.MethodHead, "/w/Article", nil))

	if next.calls != 1 {
		t.Errorf("upstream saw %d requests, want 1", next.calls)
	}
}

// PURGE drops the entry only for an address in purge_acl.
func TestPurge(t *testing.T) {
	for name, test := range map[string]struct {
		remote string
		status int
		purged bool
	}{
		"inside the acl":  {"10.1.2.3:4567", http.StatusNoContent, true},
		"outside the acl": {"192.0.2.1:4567", http.StatusMethodNotAllowed, false},
	} {
		t.Run(name, func(t *testing.T) {
			h, b := newTestHandler()
			b["/w/Article"] = "cached"
			r := httptest.NewRequest("PURGE", "/w/Article", nil)
			r.RemoteAddr = test.remote

			w := serve(t, h, &upstream{}, r)

			if w.Code != test.status {
				t.Errorf("status %d, want %d", w.Code, test.status)
			}
			if _, held := b["/w/Article"]; held == test.purged {
				t.Errorf("entry held: %t, want %t", held, !test.purged)
			}
		})
	}
}

// Methods other than GET, HEAD and PURGE go straight upstream.
func TestOtherMethodsPassThrough(t *testing.T) {
	h, b := newTestHandler()
	next := cacheableUpstream()

	serve(t, h, next, httptest.NewRequest(http.MethodPost, "/w/Article", nil))
	serve(t, h, next, httptest.NewRequest(http.MethodPost, "/w/Article", nil))

	if next.calls != 2 || len(b) != 0 {
		t.Errorf("upstream saw %d requests and %d entries were stored, want 2 and 0", next.calls, len(b))
	}
}
