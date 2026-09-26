package mwcache

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A response that says it must not be shared, or that the cache cannot
// revalidate, is passed through and not stored.
func TestUncacheableResponsesAreNotStored(t *testing.T) {
	for name, next := range map[string]*upstream{
		"no Cache-Control": {status: http.StatusOK},
		"private":          {status: http.StatusOK, header: http.Header{"Cache-Control": {"private, must-revalidate, max-age=0"}}},
		"no-cache":         {status: http.StatusOK, header: http.Header{"Cache-Control": {"no-cache"}}},
		"Set-Cookie": {status: http.StatusOK, header: http.Header{
			"Cache-Control": {"s-maxage=18000"},
			"Set-Cookie":    {"session=1"},
		}},
		"server error": {status: http.StatusInternalServerError, header: http.Header{"Cache-Control": {"s-maxage=18000"}}},
		"not modified": {status: http.StatusNotModified, header: http.Header{"Cache-Control": {"s-maxage=18000"}}},
	} {
		t.Run(name, func(t *testing.T) {
			h, b := newTestHandler()
			serve(t, h, next, httptest.NewRequest(http.MethodGet, "/w/Article", nil))
			serve(t, h, next, httptest.NewRequest(http.MethodGet, "/w/Article", nil))
			if len(b) != 0 {
				t.Errorf("stored %v", b)
			}
			if next.calls != 2 {
				t.Errorf("upstream saw %d requests, want 2", next.calls)
			}
		})
	}
}

// A request carrying a session or credentials is personal, so it goes past the
// cache in both directions.
func TestPersonalRequestsSkipTheCache(t *testing.T) {
	for name, prepare := range map[string]func(*http.Request){
		"session cookie": func(r *http.Request) { r.Header.Set("Cookie", "femiwiki_session=abc") },
		"token cookie":   func(r *http.Request) { r.Header.Set("Cookie", "femiwikiToken=abc") },
		"basic auth":     func(r *http.Request) { r.SetBasicAuth("user", "password") },
	} {
		t.Run(name, func(t *testing.T) {
			h, b := newTestHandler()
			next := cacheableUpstream()
			serve(t, h, next, httptest.NewRequest(http.MethodGet, "/w/Article", nil))

			r := httptest.NewRequest(http.MethodGet, "/w/Article", nil)
			prepare(r)
			serve(t, h, next, r)

			if next.calls != 2 {
				t.Errorf("upstream saw %d requests, want 2", next.calls)
			}
			if len(b) != 1 {
				t.Errorf("%d entries stored, want the anonymous one only", len(b))
			}
		})
	}
}
