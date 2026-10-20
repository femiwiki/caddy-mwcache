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

// An entry older than its s-maxage is fetched again and replaced.
func TestStaleEntryIsRefetched(t *testing.T) {
	h, b := newTestHandler()
	var stale bytes.Buffer
	if err := gob.NewEncoder(&stale).Encode(metadata{
		Header: http.Header{
			"Cache-Control": {"s-maxage=60"},
			"Date":          {time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)},
		},
		Status: http.StatusOK,
	}); err != nil {
		t.Fatal(err)
	}
	stale.WriteString("old")
	b[articleKey] = stale.String()
	next := cacheableUpstream()

	w := serveCached(t, h, next, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil))

	if next.calls != 1 {
		t.Errorf("upstream saw %d requests, want 1", next.calls)
	}
	if w.Body.String() != "article" {
		t.Errorf("body is %q, want the refetched %q", w.Body.String(), "article")
	}
	if b[articleKey] == stale.String() {
		t.Error("the stale entry was not replaced")
	}
}

func TestIsFresh(t *testing.T) {
	now := time.Now().UTC()
	for name, test := range map[string]struct {
		header http.Header
		fresh  bool
	}{
		"within s-maxage":  {http.Header{"Cache-Control": {"s-maxage=60"}, "Date": {now.Format(http.TimeFormat)}}, true},
		"past s-maxage":    {http.Header{"Cache-Control": {"s-maxage=60"}, "Date": {now.Add(-time.Hour).Format(http.TimeFormat)}}, false},
		"no Cache-Control": {http.Header{"Date": {now.Add(-time.Hour).Format(http.TimeFormat)}}, true},
		"no s-maxage":      {http.Header{"Cache-Control": {"max-age=60"}, "Date": {now.Add(-time.Hour).Format(http.TimeFormat)}}, true},
		"s-maxage too big": {http.Header{"Cache-Control": {"s-maxage=99999999999"}, "Date": {now.Add(-time.Hour).Format(http.TimeFormat)}}, true},
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

// ttlBackend is a mapBackend that keeps the ttl each entry was put with.
type ttlBackend struct {
	mapBackend
	ttls map[string]time.Duration
}

func (b ttlBackend) put(key string, val string, ttl time.Duration) error {
	b.ttls[key] = ttl
	return b.mapBackend.put(key, val, ttl)
}

// An entry is put with what is left of its s-maxage, so the backend lets it go
// once it is stale.
func TestEntryIsPutWithItsFreshnessLifetime(t *testing.T) {
	for name, test := range map[string]struct {
		cacheControl string
		date         time.Time
		stored       bool
		min, max     time.Duration
	}{
		"s-maxage from now":  {"s-maxage=300", time.Now(), true, 299 * time.Second, 300 * time.Second},
		"s-maxage part gone": {"s-maxage=300", time.Now().Add(-time.Minute), true, 239 * time.Second, 240 * time.Second},
		"no s-maxage":        {"public, max-age=300", time.Now(), true, 0, 0},
		"s-maxage=0":         {"s-maxage=0", time.Now(), false, 0, 0},
		"already stale":      {"s-maxage=60", time.Now().Add(-time.Hour), false, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			b := ttlBackend{mapBackend{}, map[string]time.Duration{}}
			h := Handler{logger: zap.NewNop(), backend: b}
			next := &upstream{
				status: http.StatusOK,
				header: http.Header{
					"Cache-Control": {test.cacheControl},
					"Date":          {test.date.UTC().Format(http.TimeFormat)},
				},
				body: "article",
			}

			w := serveCached(t, h, next, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Article", nil))

			if w.Body.String() != "article" {
				t.Errorf("body is %q, want %q", w.Body.String(), "article")
			}
			ttl, stored := b.ttls[articleKey]
			if stored != test.stored {
				t.Fatalf("stored: %t, want %t", stored, test.stored)
			}
			if ttl < test.min || ttl > test.max {
				t.Errorf("ttl is %s, want between %s and %s", ttl, test.min, test.max)
			}
		})
	}
}

func TestFreshFor(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for name, test := range map[string]struct {
		header   http.Header
		lifetime time.Duration
		ok       bool
	}{
		"within s-maxage":  {http.Header{"Cache-Control": {"s-maxage=60"}, "Date": {now.Add(-10 * time.Second).Format(http.TimeFormat)}}, 50 * time.Second, true},
		"past s-maxage":    {http.Header{"Cache-Control": {"s-maxage=60"}, "Date": {now.Add(-time.Hour).Format(http.TimeFormat)}}, -59 * time.Minute, true},
		"no Cache-Control": {http.Header{"Date": {now.Format(http.TimeFormat)}}, 0, false},
		"no s-maxage":      {http.Header{"Cache-Control": {"max-age=60"}, "Date": {now.Format(http.TimeFormat)}}, 0, false},
		"no Date":          {http.Header{"Cache-Control": {"s-maxage=60"}}, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			h, _ := newTestHandler()
			lifetime, ok := h.freshFor(test.header, now)
			if lifetime != test.lifetime || ok != test.ok {
				t.Errorf("freshFor: %s, %t, want %s, %t", lifetime, ok, test.lifetime, test.ok)
			}
		})
	}
}
