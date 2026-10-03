package mwcache

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func TestRedirectWithEmptyBody(t *testing.T) {
	b, err := newRistrettoBackend(map[string]string{"num_counters": "100", "max_cost_bytes": "1000000", "buffer_items": "64"})
	if err != nil {
		t.Fatal(err)
	}
	h := Handler{logger: zap.NewNop(), backend: b}
	calls := 0
	upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		calls++
		w.Header().Set("Cache-Control", "s-maxage=1200, must-revalidate, max-age=0")
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("Location", "https://femiwiki.com/w/Main")
		w.WriteHeader(http.StatusMovedPermanently)
		return nil
	})

	for i, acceptEncoding := range []string{"gzip", "gzip", ""} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}
		rec := httptest.NewRecorder()
		if err := h.ServeHTTP(rec, req, upstream); err != nil {
			t.Fatal(err)
		}
		b.wait()
		if rec.Code != http.StatusMovedPermanently {
			t.Errorf("Test %d: expected 301, got %d", i, rec.Code)
		}
		if got := rec.Header().Get("Location"); got != "https://femiwiki.com/w/Main" {
			t.Errorf("Test %d: Location %q", i, got)
		}
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("Test %d: expected no encoding on an empty body, got %q", i, got)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("Test %d: expected an empty body, got %d bytes", i, rec.Body.Len())
		}
	}
	if calls != 1 {
		t.Errorf("Expected one call upstream but got %d", calls)
	}
}
