package mwcache

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func TestEntriesAreStoredCompressed(t *testing.T) {
	b, err := newRistrettoBackend(map[string]string{"num_counters": "100", "max_cost_bytes": "1000000", "buffer_items": "64"})
	if err != nil {
		t.Fatal(err)
	}
	h := Handler{logger: zap.NewNop(), backend: b}
	page := strings.Repeat("<p>femiwiki</p>\n", 500)
	calls := 0
	upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		calls++
		w.Header().Set("Cache-Control", "s-maxage=18000, must-revalidate, max-age=0")
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("Content-Length", "8000")
		_, err := io.WriteString(w, page)
		return err
	})
	get := func(acceptEncoding string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/w/Page", nil)
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}
		rec := httptest.NewRecorder()
		if err := h.ServeHTTP(rec, req, upstream); err != nil {
			t.Fatal(err)
		}
		b.wait()
		return rec
	}
	gunzip := func(b []byte) string {
		t.Helper()
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		out, err := io.ReadAll(zr)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}

	for i, test := range []struct {
		acceptEncoding string
		gzipped        bool
	}{
		{"gzip, deflate, br", true}, // miss
		{"gzip, deflate, br", true}, // hit
		{"", false},
		{"gzip;q=0, identity", false},
		{"*", true},
	} {
		rec := get(test.acceptEncoding)
		if got := rec.Header().Get("Content-Length"); got != "" {
			t.Errorf("Test %d: Content-Length %q left over", i, got)
		}
		body := rec.Body.String()
		if test.gzipped {
			if rec.Header().Get("Content-Encoding") != "gzip" {
				t.Fatalf("Test %d: expected gzip, got %q", i, rec.Header().Get("Content-Encoding"))
			}
			body = gunzip(rec.Body.Bytes())
		} else if rec.Header().Get("Content-Encoding") != "" {
			t.Fatalf("Test %d: expected no encoding, got %q", i, rec.Header().Get("Content-Encoding"))
		}
		if body != page {
			t.Errorf("Test %d: body differs from the page", i)
		}
	}
	if calls != 1 {
		t.Errorf("Expected one call upstream but got %d", calls)
	}
	stored, err := b.get("example.com/w/Page")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) >= len(page)/4 {
		t.Errorf("Expected the entry to be compressed, but it takes %d bytes for a %d byte page", len(stored), len(page))
	}
}
