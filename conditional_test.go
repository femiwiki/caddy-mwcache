package mwcache

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

const (
	lastModified = "Sat, 17 Oct 2026 12:00:00 GMT"
	articleETag  = `W/"81532-20261017120000"`
)

func conditionalUpstream() *upstream {
	u := cacheableUpstream()
	u.header.Set("Content-Type", "text/html; charset=UTF-8")
	u.header.Set("Last-Modified", lastModified)
	u.header.Set("ETag", articleETag)
	return u
}

// A hit answers a conditional request the stored response satisfies with a
// 304 and no body, and any other with the stored 200.
func TestConditionalHit(t *testing.T) {
	for _, test := range []struct {
		name   string
		header map[string]string
		want   int
	}{
		{"none", nil, http.StatusOK},
		{"matching etag", map[string]string{"If-None-Match": articleETag}, http.StatusNotModified},
		{"strong form of the weak etag", map[string]string{"If-None-Match": `"81532-20261017120000"`}, http.StatusNotModified},
		{"etag in a list", map[string]string{"If-None-Match": `"other", ` + articleETag}, http.StatusNotModified},
		{"star", map[string]string{"If-None-Match": "*"}, http.StatusNotModified},
		{"other etag", map[string]string{"If-None-Match": `"other"`}, http.StatusOK},
		{"since last modified", map[string]string{"If-Modified-Since": lastModified}, http.StatusNotModified},
		{"since before last modified", map[string]string{"If-Modified-Since": "Fri, 16 Oct 2026 12:00:00 GMT"}, http.StatusOK},
		{"unparsable date", map[string]string{"If-Modified-Since": "yesterday"}, http.StatusOK},
		// If-None-Match wins over If-Modified-Since
		{"other etag, since last modified", map[string]string{"If-None-Match": `"other"`, "If-Modified-Since": lastModified}, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, _ := newTestHandler()
			next := conditionalUpstream()
			serveCached(t, h, next, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil))

			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil)
			r.Header.Set("Accept-Encoding", "gzip")
			for k, v := range test.header {
				r.Header.Set(k, v)
			}
			w := serveCached(t, h, next, r)

			if next.calls != 1 {
				t.Errorf("upstream saw %d requests, want 1", next.calls)
			}
			if w.Code != test.want {
				t.Fatalf("status %d, want %d", w.Code, test.want)
			}
			if w.Header().Get("ETag") != articleETag {
				t.Errorf("ETag is %q", w.Header().Get("ETag"))
			}
			if test.want != http.StatusNotModified {
				return
			}
			if w.Body.Len() != 0 {
				t.Errorf("a 304 has a body of %d bytes", w.Body.Len())
			}
			for _, k := range []string{"Content-Type", "Content-Length", "Content-Encoding"} {
				if v := w.Header().Get(k); v != "" {
					t.Errorf("a 304 has %s: %s", k, v)
				}
			}
			if w.Header().Get("Cache-Control") != next.header.Get("Cache-Control") {
				t.Errorf("Cache-Control is %q", w.Header().Get("Cache-Control"))
			}
		})
	}
}

// A HEAD is answered the same way as a GET.
func TestConditionalHeadHit(t *testing.T) {
	h, _ := newTestHandler()
	next := conditionalUpstream()
	serveCached(t, h, next, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil))
	r := httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/w/Article", nil)
	r.Header.Set("If-None-Match", articleETag)
	if w := serveCached(t, h, next, r); w.Code != http.StatusNotModified {
		t.Errorf("status %d, want 304", w.Code)
	}
}

// Without a Last-Modified, the stored Date stands in for it.
func TestIfModifiedSinceFallsBackToDate(t *testing.T) {
	h, _ := newTestHandler()
	next := cacheableUpstream()
	first := serveCached(t, h, next, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil))
	date, err := http.ParseTime(first.Header().Get("Date"))
	if err != nil {
		t.Fatal(err)
	}
	for since, want := range map[time.Time]int{
		date:                   http.StatusNotModified,
		date.Add(-time.Second): http.StatusOK,
	} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil)
		r.Header.Set("If-Modified-Since", since.Format(http.TimeFormat))
		if w := serveCached(t, h, next, r); w.Code != want {
			t.Errorf("since %s: status %d, want %d", since, w.Code, want)
		}
	}
}

// A stored redirect is sent as it is, conditional or not.
func TestConditionalRedirectIsSent(t *testing.T) {
	h, _ := newTestHandler()
	next := conditionalUpstream()
	next.status = http.StatusMovedPermanently
	next.header.Set("Location", "https://femiwiki.com/w/Main")
	serveCached(t, h, next, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil))
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil)
	r.Header.Set("If-None-Match", articleETag)
	if w := serveCached(t, h, next, r); w.Code != http.StatusMovedPermanently {
		t.Errorf("status %d, want 301", w.Code)
	}
}

// A hit says how long ago the stored response was made; the response that
// fills the cache does not.
func TestAgeHeader(t *testing.T) {
	h, b := newTestHandler()
	next := cacheableUpstream()
	date := time.Now().Add(-90 * time.Second).UTC()
	next.header.Set("Date", date.Format(http.TimeFormat))

	first := serveCached(t, h, next, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil))
	if age := first.Header().Get("Age"); age != "" {
		t.Errorf("the response that filled the cache has Age: %s", age)
	}
	second := serveCached(t, h, next, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil))
	age, err := strconv.Atoi(second.Header().Get("Age"))
	if err != nil {
		t.Fatalf("Age is %q", second.Header().Get("Age"))
	}
	if age < 90 || age > 95 {
		t.Errorf("Age is %d, want about 90", age)
	}
	if entry, _ := b.get(articleKey); entry == "" {
		t.Fatal("nothing was stored")
	}
}

func TestSetAge(t *testing.T) {
	now := time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)
	for date, want := range map[string]string{
		now.Add(-time.Hour).Format(http.TimeFormat): "3600",
		// A Date ahead of the clock is not a negative age
		now.Add(time.Minute).Format(http.TimeFormat): "0",
		"not a date": "",
	} {
		header := http.Header{"Date": {date}}
		setAge(header, now)
		if got := header.Get("Age"); got != want {
			t.Errorf("Date %q: Age %q, want %q", date, got, want)
		}
	}
}
