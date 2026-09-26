package mwcache

import (
	"bytes"
	"encoding/gob"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// An entry older than its s-maxage is fetched again and replaced.
func TestStaleEntryIsRefetched(t *testing.T) {
	h, b := newTestHandler()
	var stale bytes.Buffer
	if err := gob.NewEncoder(&stale).Encode(metadata{
		Header: http.Header{
			"Cache-Control": {"s-maxage=60"},
			"Date":          {time.Now().Add(-time.Hour).UTC().Format(timeFormat)},
		},
		Status: http.StatusOK,
	}); err != nil {
		t.Fatal(err)
	}
	stale.WriteString("old")
	b["/w/Article"] = stale.String()
	next := cacheableUpstream()

	w := serve(t, h, next, httptest.NewRequest(http.MethodGet, "/w/Article", nil))

	if next.calls != 1 {
		t.Errorf("upstream saw %d requests, want 1", next.calls)
	}
	if w.Body.String() != "article" {
		t.Errorf("body is %q, want the refetched %q", w.Body.String(), "article")
	}
	if b["/w/Article"] == stale.String() {
		t.Error("the stale entry was not replaced")
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

func TestIsFresh(t *testing.T) {
	now := time.Now().UTC()
	for name, test := range map[string]struct {
		header http.Header
		fresh  bool
	}{
		"within s-maxage":  {http.Header{"Cache-Control": {"s-maxage=60"}, "Date": {now.Format(timeFormat)}}, true},
		"past s-maxage":    {http.Header{"Cache-Control": {"s-maxage=60"}, "Date": {now.Add(-time.Hour).Format(timeFormat)}}, false},
		"no Cache-Control": {http.Header{"Date": {now.Add(-time.Hour).Format(timeFormat)}}, true},
		"no s-maxage":      {http.Header{"Cache-Control": {"max-age=60"}, "Date": {now.Add(-time.Hour).Format(timeFormat)}}, true},
		"s-maxage too big": {http.Header{"Cache-Control": {"s-maxage=99999999999"}, "Date": {now.Add(-time.Hour).Format(timeFormat)}}, true},
		"no Date":          {http.Header{"Cache-Control": {"s-maxage=60"}}, true},
		"unreadable Date":  {http.Header{"Cache-Control": {"s-maxage=60"}, "Date": {"yesterday"}}, true},
	} {
		t.Run(name, func(t *testing.T) {
			h, _ := newTestHandler()
			if fresh := h.isFresh(test.header); fresh != test.fresh {
				t.Errorf("fresh: %t, want %t", fresh, test.fresh)
			}
		})
	}
}

// The backend outlives a config reload, and is rebuilt only when its options
// change.
func TestSharedBackend(t *testing.T) {
	options := map[string]string{"num_counters": "1000", "max_cost": "100", "buffer_items": "64"}
	first, err := sharedBackend(Config{Backend: "ristretto", RistrettoConfig: options})
	if err != nil {
		t.Fatal(err)
	}
	again, err := sharedBackend(Config{Backend: "ristretto", RistrettoConfig: options})
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Error("the same options built a new backend")
	}
	changed, err := sharedBackend(Config{Backend: "ristretto", RistrettoConfig: map[string]string{
		"num_counters": "1000", "max_cost": "200", "buffer_items": "64",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Error("changed options kept the old backend")
	}

	for _, name := range []string{"", "redis"} {
		if _, err := sharedBackend(Config{Backend: name}); err == nil {
			t.Errorf("backend %q was accepted", name)
		}
	}
}
