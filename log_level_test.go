package mwcache

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestServingLogsNothingAtInfo(t *testing.T) {
	for _, test := range []struct {
		name         string
		cacheControl string
		date         string
		cookie       string
	}{
		{name: "hit", cacheControl: "public, max-age=300, s-maxage=300"},
		{name: "stale", cacheControl: "public, max-age=0, s-maxage=0"},
		{name: "no s-maxage", cacheControl: "public, max-age=300"},
		{name: "s-maxage overflows", cacheControl: "public, s-maxage=99999999999"},
		{name: "bad Date", cacheControl: "public, s-maxage=300", date: "yesterday"},
		{name: "private response", cacheControl: "private, s-maxage=300"},
		{name: "session cookie", cacheControl: "public, s-maxage=300", cookie: "femiwiki_session=x"},
	} {
		b, err := newRistrettoBackend(map[string]string{"num_counters": "100", "max_cost_bytes": "1000000", "buffer_items": "64"})
		if err != nil {
			t.Fatal(err)
		}
		core, logs := observer.New(zapcore.InfoLevel)
		h := Handler{logger: zap.New(core), backend: b, Config: Config{PurgeAcl: []string{"192.0.2.0/24"}}}
		next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
			w.Header().Set("Cache-Control", test.cacheControl)
			if test.date != "" {
				w.Header().Set("Date", test.date)
			}
			_, err := w.Write([]byte("<p>page</p>"))
			return err
		})

		// httptest.NewRequest comes from 192.0.2.1
		for _, method := range []string{http.MethodGet, http.MethodGet, "PURGE"} {
			req := httptest.NewRequest(method, "/w/Main", nil)
			if test.cookie != "" {
				req.Header.Set("Cookie", test.cookie)
			}
			if err := h.ServeHTTP(httptest.NewRecorder(), req, next); err != nil {
				t.Fatal(err)
			}
			b.wait()
		}
		for _, entry := range logs.All() {
			t.Errorf("%s: logged %q at %s", test.name, entry.Message, entry.Level)
		}
	}

	// serveAndCache never stores an entry without these, so ask isFresh directly
	core, logs := observer.New(zapcore.InfoLevel)
	h := Handler{logger: zap.New(core)}
	h.isFresh(http.Header{})
	h.isFresh(http.Header{"Cache-Control": {"s-maxage=300"}})
	for _, entry := range logs.All() {
		t.Errorf("isFresh: logged %q at %s", entry.Message, entry.Level)
	}
}
