package mwcache

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

type hostTest struct {
	h     Handler
	b     *RistrettoBackend
	calls []string
	next  caddyhttp.Handler
}

// The upstream answers with the host it was asked for, the way MediaWiki does
// when $wgServer is left to be detected from the request.
func newHostTest(t *testing.T) *hostTest {
	t.Helper()
	b, err := newRistrettoBackend(map[string]string{"num_counters": "100", "max_cost_bytes": "1000000", "buffer_items": "64"})
	if err != nil {
		t.Fatal(err)
	}
	ht := &hostTest{
		h: Handler{logger: zap.NewNop(), backend: b, Config: Config{PurgeAcl: []string{"127.0.0.1"}}},
		b: b,
	}
	ht.next = caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		ht.calls = append(ht.calls, r.Host)
		w.Header().Set("Cache-Control", "s-maxage=300, must-revalidate, max-age=0")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("https://" + r.Host + "/w/Main"))
		return err
	})
	return ht
}

func (ht *hostTest) serve(t *testing.T, method string, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/w/Main", nil)
	req.Host = host
	req.RemoteAddr = "127.0.0.1:34567"
	rec := httptest.NewRecorder()
	if err := ht.h.ServeHTTP(rec, req, ht.next); err != nil {
		t.Fatal(err)
	}
	ht.b.wait()
	return rec
}

func TestHostsDoNotShareEntries(t *testing.T) {
	ht := newHostTest(t)

	ht.serve(t, http.MethodGet, "femiwiki.com")
	if rec := ht.serve(t, http.MethodGet, "m.femiwiki.com"); rec.Body.String() != "https://m.femiwiki.com/w/Main" {
		t.Errorf("m.femiwiki.com was served %q", rec.Body.String())
	}
	if rec := ht.serve(t, http.MethodGet, "FemiWiki.com:443"); rec.Body.String() != "https://femiwiki.com/w/Main" {
		t.Errorf("FemiWiki.com:443 was served %q", rec.Body.String())
	}
	if want := []string{"femiwiki.com", "m.femiwiki.com"}; !slices.Equal(ht.calls, want) {
		t.Errorf("expected upstream calls %v, got %v", want, ht.calls)
	}
}

func TestPurgeDeletesOnlyItsHost(t *testing.T) {
	ht := newHostTest(t)

	ht.serve(t, http.MethodGet, "femiwiki.com")
	ht.serve(t, http.MethodGet, "m.femiwiki.com")
	if rec := ht.serve(t, "PURGE", "m.femiwiki.com:80"); rec.Code != http.StatusNoContent {
		t.Fatalf("PURGE: expected 204, got %d", rec.Code)
	}
	if _, err := ht.b.get("m.femiwiki.com/w/Main"); err != ErrKeyNotFound {
		t.Errorf("the purged entry is still there: %v", err)
	}
	if _, err := ht.b.get("femiwiki.com/w/Main"); err != nil {
		t.Errorf("a PURGE for another host deleted this entry: %v", err)
	}
}

func TestAliasesShareEntries(t *testing.T) {
	ht := newHostTest(t)
	ht.h.Config.HostAliases = map[string]string{"www.femiwiki.com": "femiwiki.com", "127.0.0.1": "femiwiki.com"}

	ht.serve(t, http.MethodGet, "femiwiki.com")
	if rec := ht.serve(t, http.MethodGet, "WWW.femiwiki.com"); rec.Body.String() != "https://femiwiki.com/w/Main" {
		t.Errorf("www.femiwiki.com was served %q", rec.Body.String())
	}
	if want := []string{"femiwiki.com"}; !slices.Equal(ht.calls, want) {
		t.Errorf("expected upstream calls %v, got %v", want, ht.calls)
	}
}

// MediaWiki sends its PURGEs to $wgInternalServer, so the Host is 127.0.0.1
// whichever host the page was read on.
func TestPurgeThroughAnAlias(t *testing.T) {
	ht := newHostTest(t)
	ht.h.Config.HostAliases = map[string]string{"www.femiwiki.com": "femiwiki.com", "127.0.0.1": "femiwiki.com"}

	ht.serve(t, http.MethodGet, "www.femiwiki.com")
	ht.serve(t, http.MethodGet, "m.femiwiki.com")
	if rec := ht.serve(t, "PURGE", "127.0.0.1:80"); rec.Code != http.StatusNoContent {
		t.Fatalf("PURGE: expected 204, got %d", rec.Code)
	}
	if _, err := ht.b.get("femiwiki.com/w/Main"); err != ErrKeyNotFound {
		t.Errorf("the purged entry is still there: %v", err)
	}
	if _, err := ht.b.get("m.femiwiki.com/w/Main"); err != nil {
		t.Errorf("a PURGE for another host deleted this entry: %v", err)
	}
	ht.serve(t, http.MethodGet, "femiwiki.com")
	if want := []string{"www.femiwiki.com", "m.femiwiki.com", "femiwiki.com"}; !slices.Equal(ht.calls, want) {
		t.Errorf("expected upstream calls %v, got %v", want, ht.calls)
	}
}

func TestHostWithSlashIsUncacheable(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/w/X", nil)
	req.Host = "a/b.femiwiki.com"
	if requestIsCacheable(req) {
		t.Error("a host with a slash should bypass the cache")
	}
}
