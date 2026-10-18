package mwcache

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// purgeWhileRendering returns an upstream that, before it answers, has a
// PURGE for target reach h, the way an edit saved during a slow render is.
func purgeWhileRendering(t *testing.T, h Handler, target string) caddyhttp.Handler {
	t.Helper()
	return caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		purge := httptest.NewRequestWithContext(t.Context(), "PURGE", target, nil)
		purge.RemoteAddr = "10.1.2.3:4567"
		serveCached(t, h, &upstream{}, purge)
		w.Header().Set("Cache-Control", "s-maxage=18000, must-revalidate, max-age=0")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("before the edit"))
		return err
	})
}

// A render that a PURGE overtook holds the page from before the edit, so it
// is served but not stored.
func TestPurgeDuringRenderIsNotUndone(t *testing.T) {
	h, b := newTestHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil)

	if err := h.ServeHTTP(w, r, purgeWhileRendering(t, h, "/w/Article")); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}

	if w.Code != http.StatusOK || w.Body.String() != "before the edit" {
		t.Errorf("response: %d %q, want 200 %q", w.Code, w.Body.String(), "before the edit")
	}
	if _, held := b[articleKey]; held {
		t.Error("the render that started before the purge was stored")
	}

	next := cacheableUpstream()
	serveCached(t, h, next, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil))
	if _, held := b[articleKey]; !held {
		t.Error("the render after the purge was not stored")
	}
}

// A PURGE of another page does not stop the render from being stored.
func TestPurgeOfAnotherPageDuringRender(t *testing.T) {
	h, b := newTestHandler()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil)

	if err := h.ServeHTTP(httptest.NewRecorder(), r, purgeWhileRendering(t, h, "/w/Other")); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}

	if _, held := b[articleKey]; !held {
		t.Error("the render was not stored")
	}
}

// purgingBackend has a PURGE reach the handler right after each put, the
// moment between the render's check and its entry landing.
type purgingBackend struct {
	mapBackend
	purge func()
}

func (p purgingBackend) put(key string, val string) error {
	err := p.mapBackend.put(key, val)
	p.purge()
	return err
}

// A PURGE that lands while the entry is being stored still wins.
func TestPurgeDuringPutIsNotUndone(t *testing.T) {
	h, b := newTestHandler()
	h.backend = purgingBackend{b, func() { renders.purge(articleKey) }}

	serveCached(t, h, cacheableUpstream(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil))

	if _, held := b[articleKey]; held {
		t.Error("the entry stored as the purge came in was kept")
	}
}
