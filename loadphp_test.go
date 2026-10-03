package mwcache

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

const sessionCookie = "femiwiki_session=abc; femiwikiUserID=1"

func TestRequestIsCacheable(t *testing.T) {
	for _, test := range []struct {
		target   string
		cookie   string
		expected bool
	}{
		{"/load.php?modules=startup&only=scripts", "", true},
		{"/load.php?modules=startup&only=scripts", sessionCookie, true},
		{"/load.php?modules=site.styles&only=styles", "femiwikiToken=abc", true},
		{"/load.php?modules=user.options&lang=ko", sessionCookie, true},
		{"/load.php?modules=user&user=Alice&version=abc", sessionCookie, false},
		{"/load.php?modules=user&user=Alice&version=abc", "", false},
		{"/load.php?modules=user&%75ser=Alice", sessionCookie, false},
		{"/load.php?modules=user&user[]=Alice", sessionCookie, false},
		{"/load.php?modules=user&+user=Alice", sessionCookie, false},
		{"/load.php?modules=user&User=Alice", sessionCookie, false},
		{"/load.php?modules=user&user%zz=Alice", sessionCookie, false},
		{"/load.php?modules=user;user=Alice", sessionCookie, false},
		{"/load.php?modules=user&%75ser%00%zz=Alice", sessionCookie, false},
		{"/load.php?modules=startup&x%zz=1", sessionCookie, false},
		{"/w/load.php", sessionCookie, false},
		{"/w/load.php", "", true},
		{"/index.php/load.php", sessionCookie, false},
		{"/load.php/x", sessionCookie, false},
		{"/w/Main", sessionCookie, false},
		{"/w/Main", "femiwikiToken=abc", false},
		{"/w/Main", "", true},
	} {
		req := httptest.NewRequest(http.MethodGet, test.target, nil)
		if test.cookie != "" {
			req.Header.Set("Cookie", test.cookie)
		}
		if actual := requestIsCacheable(req); actual != test.expected {
			t.Errorf("%s with cookie %q: expected %t but got %t", test.target, test.cookie, test.expected, actual)
		}
	}
}

func TestLoadPHPWithSessionCookie(t *testing.T) {
	const loadURL = "/load.php?lang=ko&modules=ext.signup&only=styles&skin=femiwiki"
	for _, test := range []struct {
		name         string
		target       string
		cacheControl string
		setCookie    string
		// whether the first request leaves an entry the second one hits
		expectStored bool
	}{
		{"public", loadURL, "public, max-age=300, s-maxage=300, stale-while-revalidate=60", "", true},
		{"private", loadURL, "private, no-cache, must-revalidate", "", false},
		{"no-store", loadURL, "no-store", "", false},
		{"Set-Cookie", loadURL, "public, max-age=300, s-maxage=300", "femiwiki_session=def", false},
		{"user=", "/load.php?modules=user&user=Alice&version=abc", "public, max-age=2592000, s-maxage=2592000", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			b, err := newRistrettoBackend(map[string]string{"num_counters": "100", "max_cost_bytes": "1000000", "buffer_items": "64"})
			if err != nil {
				t.Fatal(err)
			}
			h := Handler{logger: zap.NewNop(), backend: b}
			calls := 0
			upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				calls++
				w.Header().Set("Cache-Control", test.cacheControl)
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
				if test.setCookie != "" {
					w.Header().Set("Set-Cookie", test.setCookie)
				}
				_, err := w.Write([]byte(".signup{color:red}"))
				return err
			})

			// A logged-in request, then an anonymous one, then a logged-in one again
			for i, cookie := range []string{sessionCookie, "", sessionCookie} {
				req := httptest.NewRequest(http.MethodGet, test.target, nil)
				if cookie != "" {
					req.Header.Set("Cookie", cookie)
				}
				rec := httptest.NewRecorder()
				if err := h.ServeHTTP(rec, req, upstream); err != nil {
					t.Fatal(err)
				}
				b.wait()
				if rec.Code != http.StatusOK {
					t.Errorf("request %d: expected 200, got %d", i, rec.Code)
				}
				if got := rec.Body.String(); got != ".signup{color:red}" {
					t.Errorf("request %d: body %q", i, got)
				}
			}
			expectedCalls := 3
			if test.expectStored {
				expectedCalls = 1
			}
			if calls != expectedCalls {
				t.Errorf("expected %d calls upstream but got %d", expectedCalls, calls)
			}
		})
	}
}

func TestPageWithSessionCookieSkipsTheCache(t *testing.T) {
	b, err := newRistrettoBackend(map[string]string{"num_counters": "100", "max_cost_bytes": "1000000", "buffer_items": "64"})
	if err != nil {
		t.Fatal(err)
	}
	h := Handler{logger: zap.NewNop(), backend: b}
	upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Cache-Control", "s-maxage=1200, must-revalidate, max-age=0")
		if r.Header.Get("Cookie") != "" {
			_, err := w.Write([]byte("logged in"))
			return err
		}
		_, err := w.Write([]byte("anonymous"))
		return err
	})

	for _, test := range []struct {
		cookie   string
		expected string
	}{
		{"", "anonymous"},
		{sessionCookie, "logged in"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/w/load.php", nil)
		if test.cookie != "" {
			req.Header.Set("Cookie", test.cookie)
		}
		rec := httptest.NewRecorder()
		if err := h.ServeHTTP(rec, req, upstream); err != nil {
			t.Fatal(err)
		}
		b.wait()
		if got := rec.Body.String(); got != test.expected {
			t.Errorf("cookie %q: expected %q, got %q", test.cookie, test.expected, got)
		}
	}
}
