package mwcache

import (
	"bytes"
	"encoding/gob"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
