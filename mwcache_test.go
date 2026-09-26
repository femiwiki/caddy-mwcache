package mwcache

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestCIDRContainsIP(t *testing.T) {
	for i, test := range []struct {
		cidr     string
		ip       string
		expected bool
	}{
		{"10.0.0.0/8", "10.0.0.4", true},
		{"10.0.0.0/8", "111.0.0.4", false},
		{"10.0.0.0/8", "111.0.0.4:34567", false},
		{"127.0.0.1", "127.0.0.1", true},
		{"127.0.0.1", "127.0.0.1:4567", true},
	} {
		actual := CIDRContainsIP(test.cidr, test.ip)
		if test.expected != actual {
			t.Errorf("Test %d: Expected: '%t' but got '%t'", i, test.expected, actual)
		}
	}
}

// mapBackend applies every write at once, where ristretto applies them
// asynchronously, so a test can read back what the handler just stored.
type mapBackend map[string]string

func (m mapBackend) get(key string) (string, error) {
	val, ok := m[key]
	if !ok {
		return "", ErrKeyNotFound
	}
	return val, nil
}

func (m mapBackend) put(key string, val string) error {
	m[key] = val
	return nil
}

func (m mapBackend) delete(key string) error {
	delete(m, key)
	return nil
}

// upstream stands in for the handler behind mwcache and counts the requests
// that reach it.
type upstream struct {
	calls  int
	status int
	header http.Header
	body   string
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, _ *http.Request) error {
	u.calls++
	for k, v := range u.header {
		w.Header()[k] = v
	}
	w.WriteHeader(u.status)
	if u.body == "" {
		return nil
	}
	_, err := w.Write([]byte(u.body))
	return err
}

func newTestHandler() (Handler, mapBackend) {
	b := mapBackend{}
	return Handler{
		logger:  zap.NewNop(),
		backend: b,
		Config:  Config{PurgeAcl: []string{"10.0.0.0/8"}},
	}, b
}

func cacheableUpstream() *upstream {
	return &upstream{
		status: http.StatusOK,
		header: http.Header{"Cache-Control": {"s-maxage=18000, must-revalidate, max-age=0"}},
		body:   "article",
	}
}

func serve(t *testing.T, h Handler, next *upstream, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	if err := h.ServeHTTP(w, r, next); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	return w
}

// A second read of a cacheable page is answered from the cache, with the
// status, headers and body the first one got.
func TestSecondReadIsServedFromCache(t *testing.T) {
	h, b := newTestHandler()
	next := cacheableUpstream()

	first := serve(t, h, next, httptest.NewRequest(http.MethodGet, "/w/Article", nil))
	if _, ok := b["/w/Article"]; !ok {
		t.Fatal("the response was not stored")
	}
	second := serve(t, h, next, httptest.NewRequest(http.MethodGet, "/w/Article", nil))

	if next.calls != 1 {
		t.Errorf("upstream saw %d requests, want 1", next.calls)
	}
	for i, w := range []*httptest.ResponseRecorder{first, second} {
		if w.Code != http.StatusOK || w.Body.String() != "article" {
			t.Errorf("response %d: %d %q, want 200 %q", i+1, w.Code, w.Body.String(), "article")
		}
		if w.Header().Get("Cache-Control") != next.header.Get("Cache-Control") {
			t.Errorf("response %d: Cache-Control is %q", i+1, w.Header().Get("Cache-Control"))
		}
	}
}
