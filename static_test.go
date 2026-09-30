package mwcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// md5("<svg/>\n") starts with these five digits.
const iconHash = "6a22e"

func newStatic(t *testing.T) (*StaticConfig, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skins"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"skins/icon.svg", "skins/entry.php"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("<svg/>\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &StaticConfig{UnversionedMaxAge: caddy.Duration(300 * 1e9)}
	if err := s.provision(caddy.Context{}); err != nil {
		t.Fatal(err)
	}
	return s, root
}

// fileServer stands in for Caddy's file_server: 404 is returned as an error,
// which Caddy hands to the error routes rather than to this writer.
func fileServer(root string) caddyhttp.Handler {
	return caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		path := caddyhttp.SanitizedPathJoin(root, r.URL.Path)
		f, err := os.Open(path)
		if err != nil {
			return caddyhttp.Error(http.StatusNotFound, err)
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
		return nil
	})
}

func serve(t *testing.T, s *StaticConfig, root string, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	repl := caddy.NewReplacer()
	repl.Set("http.vars.root", root)
	req = req.WithContext(context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl))
	rec := httptest.NewRecorder()
	if err := s.serve(rec, req, fileServer(root)); err != nil {
		var herr caddyhttp.HandlerError
		if !errors.As(err, &herr) {
			t.Fatal(err)
		}
		rec.Code = herr.StatusCode
	}
	return rec
}

func TestStaticCacheControl(t *testing.T) {
	s, root := newStatic(t)
	for _, test := range []struct {
		uri    string
		status int
		cc     string
	}{
		{"/skins/icon.svg?" + iconHash, 200, "public, s-maxage=31536000, max-age=31536000, immutable"},
		{"/skins/icon.svg?6A22E", 200, "public, s-maxage=60, max-age=60, must-revalidate"},
		{"/skins/icon.svg?00000", 200, "public, s-maxage=60, max-age=60, must-revalidate"},
		{"/skins/icon.svg", 200, "public, s-maxage=300, max-age=300, must-revalidate"},
		{"/skins/icon.svg?" + iconHash + "&x=1", 200, "public, s-maxage=300, max-age=300, must-revalidate"},
		{"/skins/missing.svg?" + iconHash, 404, ""},
		{"/skins/entry.php", 200, ""},
	} {
		rec := serve(t, s, root, httptest.NewRequest(http.MethodGet, test.uri, nil))
		if rec.Code != test.status {
			t.Errorf("%s: status %d, want %d", test.uri, rec.Code, test.status)
		}
		if got := rec.Header().Get("Cache-Control"); got != test.cc {
			t.Errorf("%s: Cache-Control %q, want %q", test.uri, got, test.cc)
		}
	}
}

func TestStaticNotModifiedAndRange(t *testing.T) {
	s, root := newStatic(t)
	uri := "/skins/icon.svg?" + iconHash
	want := "public, s-maxage=31536000, max-age=31536000, immutable"

	first := serve(t, s, root, httptest.NewRequest(http.MethodGet, uri, nil))
	req := httptest.NewRequest(http.MethodGet, uri, nil)
	req.Header.Set("If-Modified-Since", first.Header().Get("Last-Modified"))
	if rec := serve(t, s, root, req); rec.Code != http.StatusNotModified || rec.Header().Get("Cache-Control") != want {
		t.Errorf("304: got %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}

	req = httptest.NewRequest(http.MethodGet, uri, nil)
	req.Header.Set("Range", "bytes=0-2")
	if rec := serve(t, s, root, req); rec.Code != http.StatusPartialContent || rec.Header().Get("Cache-Control") != want {
		t.Errorf("206: got %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

// A file replaced in place, as a deploy does, must be hashed again.
func TestStaticRehashesChangedFile(t *testing.T) {
	s, root := newStatic(t)
	uri := "/skins/icon.svg?" + iconHash
	if cc := serve(t, s, root, httptest.NewRequest(http.MethodGet, uri, nil)).Header().Get("Cache-Control"); cc == "" {
		t.Fatal("no Cache-Control on the first request")
	}
	if err := os.WriteFile(filepath.Join(root, "skins/icon.svg"), []byte("<svg id=\"new\"/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := "public, s-maxage=60, max-age=60, must-revalidate"
	if cc := serve(t, s, root, httptest.NewRequest(http.MethodGet, uri, nil)).Header().Get("Cache-Control"); cc != want {
		t.Errorf("after the file changed: %q, want %q", cc, want)
	}
}

// Through the handler, a static path gets the header and is never stored,
// and any other path is left to the page cache as before.
func TestHandlerStatic(t *testing.T) {
	_, root := newStatic(t)
	h := &Handler{}
	if err := h.UnmarshalCaddyfile(caddyfile.NewTestDispenser(`
	mwcache {
		ristretto {
			num_counters 1000
			max_cost 100
			buffer_items 64
		}
		static
	}
	`)); err != nil {
		t.Fatal(err)
	}
	if err := h.Provision(caddy.Context{}); err != nil {
		t.Fatal(err)
	}
	if err := h.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.Config.Static.Paths, " "); got != "/resources/* /skins/* /extensions/*" {
		t.Errorf("default paths %q", got)
	}

	repl := caddy.NewReplacer()
	repl.Set("http.vars.root", root)
	for _, uri := range []string{"/skins/icon.svg?" + iconHash, "/skins/icon.svg?" + iconHash} {
		req := httptest.NewRequest(http.MethodGet, uri, nil)
		req = req.WithContext(context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl))
		rec := httptest.NewRecorder()
		if err := h.ServeHTTP(rec, req, fileServer(root)); err != nil {
			t.Fatal(err)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "public, s-maxage=31536000, max-age=31536000, immutable" {
			t.Errorf("%s: Cache-Control %q", uri, cc)
		}
		if _, err := h.backend.get(createKey(req)); err != ErrKeyNotFound {
			t.Errorf("%s: the page cache stored a static file (err = %v)", uri, err)
		}
	}
}

func TestStaticDirectives(t *testing.T) {
	for _, test := range []struct {
		caddyfile string
		valid     bool
		want      *StaticConfig
	}{
		{`mwcache`, true, nil},
		{`mwcache {
			static
		}`, true, &StaticConfig{}},
		{`mwcache {
			static {
				paths /skins/* /fw-resources/*
				unversioned_max_age 5m
				mismatch_max_age 30s
				versioned_max_age 24h
			}
		}`, true, &StaticConfig{
			Paths:             caddyhttp.MatchPath{"/skins/*", "/fw-resources/*"},
			VersionedMaxAge:   caddy.Duration(24 * 3600 * 1e9),
			UnversionedMaxAge: caddy.Duration(300 * 1e9),
			MismatchMaxAge:    caddy.Duration(30 * 1e9),
		}},
		{`mwcache {
			static foo
		}`, false, nil},
		{`mwcache {
			static {
				paths
			}
		}`, false, nil},
		{`mwcache {
			static {
				unversioned_max_age
			}
		}`, false, nil},
		{`mwcache {
			static {
				unversioned_max_age soon
			}
		}`, false, nil},
		{`mwcache {
			static {
				max_age 5m
			}
		}`, false, nil},
	} {
		var h Handler
		err := h.UnmarshalCaddyfile(caddyfile.NewTestDispenser(test.caddyfile))
		if test.valid != (err == nil) {
			t.Errorf("%s: error = %v", test.caddyfile, err)
			continue
		}
		if !test.valid {
			continue
		}
		got := h.Config.Static
		if (got == nil) != (test.want == nil) {
			t.Errorf("%s: static = %+v, want %+v", test.caddyfile, got, test.want)
			continue
		}
		if got != nil && (strings.Join(got.Paths, " ") != strings.Join(test.want.Paths, " ") ||
			got.VersionedMaxAge != test.want.VersionedMaxAge ||
			got.UnversionedMaxAge != test.want.UnversionedMaxAge ||
			got.MismatchMaxAge != test.want.MismatchMaxAge) {
			t.Errorf("%s: got %+v", test.caddyfile, got)
		}
	}
}

// A handler may write a body without calling WriteHeader, or hand the
// writer to io.Copy.
func TestStaticWriterPaths(t *testing.T) {
	s, root := newStatic(t)
	want := "public, s-maxage=31536000, max-age=31536000, immutable"
	for name, h := range map[string]caddyhttp.HandlerFunc{
		"write": func(w http.ResponseWriter, _ *http.Request) error {
			_, err := w.Write([]byte("<svg/>\n"))
			return err
		},
		"read from": func(w http.ResponseWriter, _ *http.Request) error {
			_, err := io.Copy(w, io.LimitReader(strings.NewReader("<svg/>\n"), 64))
			return err
		},
	} {
		repl := caddy.NewReplacer()
		repl.Set("http.vars.root", root)
		req := httptest.NewRequest(http.MethodGet, "/skins/icon.svg?"+iconHash, nil)
		req = req.WithContext(context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl))
		rec := httptest.NewRecorder()
		if err := s.serve(rec, req, h); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != want {
			t.Errorf("%s: got %d %q", name, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestStaticSkipsDirectoriesAndUnreadableFiles(t *testing.T) {
	s, root := newStatic(t)
	ok := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})
	if cc := serve(t, s, root, httptest.NewRequest(http.MethodGet, "/skins/?"+iconHash, nil)).Header().Get("Cache-Control"); cc != "" {
		t.Errorf("directory: %q", cc)
	}

	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	if err := os.Chmod(filepath.Join(root, "skins/icon.svg"), 0); err != nil {
		t.Fatal(err)
	}
	repl := caddy.NewReplacer()
	repl.Set("http.vars.root", root)
	req := httptest.NewRequest(http.MethodGet, "/skins/icon.svg?"+iconHash, nil)
	req = req.WithContext(context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl))
	rec := httptest.NewRecorder()
	if err := s.serve(rec, req, ok); err != nil {
		t.Fatal(err)
	}
	if cc, want := rec.Header().Get("Cache-Control"), "public, s-maxage=60, max-age=60, must-revalidate"; cc != want {
		t.Errorf("unreadable: %q, want %q", cc, want)
	}
}

func TestStaticValidate(t *testing.T) {
	for _, s := range []StaticConfig{
		{VersionedMaxAge: -1},
		{UnversionedMaxAge: -1},
		{MismatchMaxAge: -1},
	} {
		h := Handler{Config: Config{Backend: "ristretto", PurgeAcl: []string{}, Static: &s}}
		if err := h.Validate(); err == nil {
			t.Errorf("%+v: no error", s)
		}
	}
}

type statusRecorder struct {
	*httptest.ResponseRecorder
	sent []string
}

func (r *statusRecorder) WriteHeader(status int) {
	r.sent = append(r.sent, fmt.Sprintf("%d %s", status, r.Header().Get("Cache-Control")))
	r.ResponseRecorder.WriteHeader(status)
}

func TestStaticEarlyHints(t *testing.T) {
	s, root := newStatic(t)
	repl := caddy.NewReplacer()
	repl.Set("http.vars.root", root)
	req := httptest.NewRequest(http.MethodGet, "/skins/icon.svg?"+iconHash, nil)
	req = req.WithContext(context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl))
	rec := &statusRecorder{ResponseRecorder: httptest.NewRecorder()}
	err := s.serve(rec, req, caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusOK)
		w.WriteHeader(http.StatusOK)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := "103 |200 public, s-maxage=31536000, max-age=31536000, immutable|200 public, s-maxage=31536000, max-age=31536000, immutable"
	if got := strings.Join(rec.sent, "|"); got != want {
		t.Errorf("got %q", got)
	}
}
