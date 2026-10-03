package mwcache

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

const headTestURL = "/load.php?modules=site.styles&only=styles"

type headTest struct {
	h     Handler
	b     *RistrettoBackend
	calls []string
	next  caddyhttp.Handler
}

func newHeadTest(t *testing.T, cacheControl string) *headTest {
	t.Helper()
	b, err := newRistrettoBackend(map[string]string{"num_counters": "100", "max_cost_bytes": "1000000", "buffer_items": "64"})
	if err != nil {
		t.Fatal(err)
	}
	ht := &headTest{h: Handler{logger: zap.NewNop(), backend: b}, b: b}
	ht.next = caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		ht.calls = append(ht.calls, r.Method)
		w.Header().Set("Cache-Control", cacheControl)
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		// PHP-FPM sends no body for HEAD
		if r.Method == http.MethodHead {
			return nil
		}
		_, err := w.Write([]byte(".site{color:red}"))
		return err
	})
	return ht
}

func (ht *headTest) serve(t *testing.T, method string) *httptest.ResponseRecorder {
	t.Helper()
	return ht.serveWith(t, method, "")
}

func (ht *headTest) serveWith(t *testing.T, method string, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, headTestURL, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	if err := ht.h.ServeHTTP(rec, req, ht.next); err != nil {
		t.Fatal(err)
	}
	ht.b.wait()
	if rec.Code != http.StatusOK {
		t.Errorf("%s: expected 200, got %d", method, rec.Code)
	}
	return rec
}

func TestHeadDoesNotFillTheCache(t *testing.T) {
	ht := newHeadTest(t, "public, max-age=300, s-maxage=300")

	if rec := ht.serve(t, http.MethodHead); rec.Body.Len() != 0 {
		t.Errorf("HEAD: expected no body, got %d bytes", rec.Body.Len())
	}
	if _, err := ht.b.get(headTestURL); err != ErrKeyNotFound {
		t.Errorf("HEAD left an entry: %v", err)
	}
	if rec := ht.serve(t, http.MethodGet); rec.Body.String() != ".site{color:red}" {
		t.Errorf("GET after HEAD: body %q", rec.Body.String())
	}
	if want := []string{http.MethodHead, http.MethodGet}; !slices.Equal(ht.calls, want) {
		t.Errorf("expected upstream calls %v, got %v", want, ht.calls)
	}
}

func TestHeadIsServedFromTheCache(t *testing.T) {
	ht := newHeadTest(t, "public, max-age=300, s-maxage=300")

	ht.serve(t, http.MethodGet)
	for _, test := range []struct {
		acceptEncoding  string
		contentEncoding string
	}{
		{"", ""},
		{"gzip", "gzip"},
	} {
		rec := ht.serveWith(t, http.MethodHead, test.acceptEncoding)
		if rec.Body.Len() != 0 {
			t.Errorf("HEAD %q: expected no body, got %d bytes", test.acceptEncoding, rec.Body.Len())
		}
		if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
			t.Errorf("HEAD %q: Content-Type %q", test.acceptEncoding, got)
		}
		if got := rec.Header().Get("Content-Encoding"); got != test.contentEncoding {
			t.Errorf("HEAD %q: Content-Encoding %q, expected %q", test.acceptEncoding, got, test.contentEncoding)
		}
		if got := rec.Header().Get("Content-Length"); got != "" {
			t.Errorf("HEAD %q: Content-Length %q", test.acceptEncoding, got)
		}
	}
	if want := []string{http.MethodGet}; !slices.Equal(ht.calls, want) {
		t.Errorf("expected upstream calls %v, got %v", want, ht.calls)
	}
}

func TestStaleHeadDoesNotReplaceTheEntry(t *testing.T) {
	// s-maxage=0 makes the entry stale on its next read
	ht := newHeadTest(t, "public, max-age=0, s-maxage=0")

	ht.serve(t, http.MethodGet)
	before, err := ht.b.get(headTestURL)
	if err != nil {
		t.Fatal(err)
	}
	ht.serve(t, http.MethodHead)
	after, err := ht.b.get(headTestURL)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Error("a stale HEAD replaced the entry")
	}
	if want := []string{http.MethodGet, http.MethodHead}; !slices.Equal(ht.calls, want) {
		t.Errorf("expected upstream calls %v, got %v", want, ht.calls)
	}
}
