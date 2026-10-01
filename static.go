package mwcache

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// MediaWiki links a file under skins/, resources/ and extensions/ with the
// first five hex digits of its md5 as the whole query string. See
// https://github.com/wikimedia/operations-mediawiki-config/blob/63f500d0/w/static.php
var versionHash = regexp.MustCompile(`^[a-fA-F0-9]{5}$`)

// The durations static.php gives the three cases, and the trees MediaWiki
// links its files from.
const (
	defaultVersionedMaxAge   = 365 * 24 * time.Hour
	defaultUnversionedMaxAge = 365 * 24 * time.Hour
	defaultMismatchMaxAge    = time.Minute
)

var defaultStaticPaths = []string{"/resources/*", "/skins/*", "/extensions/*"}

// StaticConfig sets Cache-Control on a file read off disk by the file server,
// the way static.php does. A query that is the file's hash is kept for a year
// and marked immutable. A query that looks like a hash but is not this file's
// gets a minute, so a URL asked for before its file arrived, as when two
// servers overlap during a deploy, is not pinned to the old file. Anything
// else gets UnversionedMaxAge. Only 2xx and 304 responses from a regular file
// under the site root are touched, and the page cache never stores them.
type StaticConfig struct {
	Paths             caddyhttp.MatchPath `json:"paths,omitempty"`
	VersionedMaxAge   caddy.Duration      `json:"versioned_max_age,omitempty"`
	UnversionedMaxAge caddy.Duration      `json:"unversioned_max_age,omitempty"`
	MismatchMaxAge    caddy.Duration      `json:"mismatch_max_age,omitempty"`

	hashes *hashCache
}

func (s *StaticConfig) provision(ctx caddy.Context) error {
	if len(s.Paths) == 0 {
		s.Paths = append(caddyhttp.MatchPath{}, defaultStaticPaths...)
	}
	if err := s.Paths.Provision(ctx); err != nil {
		return err
	}
	if s.VersionedMaxAge == 0 {
		s.VersionedMaxAge = caddy.Duration(defaultVersionedMaxAge)
	}
	if s.UnversionedMaxAge == 0 {
		s.UnversionedMaxAge = caddy.Duration(defaultUnversionedMaxAge)
	}
	if s.MismatchMaxAge == 0 {
		s.MismatchMaxAge = caddy.Duration(defaultMismatchMaxAge)
	}
	s.hashes = &hashCache{entries: map[string]hashEntry{}}
	return nil
}

func (s *StaticConfig) validate() error {
	for name, d := range map[string]caddy.Duration{
		"versioned_max_age":   s.VersionedMaxAge,
		"unversioned_max_age": s.UnversionedMaxAge,
		"mismatch_max_age":    s.MismatchMaxAge,
	} {
		if d < 0 {
			return fmt.Errorf("static: %s must not be negative", name)
		}
	}
	return nil
}

// matches reports whether the request is for one of the static paths.
func (s *StaticConfig) matches(r *http.Request) bool {
	ok, err := s.Paths.MatchWithError(r)
	return err == nil && ok
}

func (s *StaticConfig) serve(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	sw := &staticWriter{ResponseWriterWrapper: &caddyhttp.ResponseWriterWrapper{ResponseWriter: w}, s: s, r: r}
	return next.ServeHTTP(sw, r)
}

// cacheControl reads the request as the file server saw it, rewrites and
// all, since the request is shared and the header goes out after it ran.
func (s *StaticConfig) cacheControl(r *http.Request) string {
	if strings.EqualFold(filepath.Ext(r.URL.Path), ".php") {
		return ""
	}
	root := "."
	if repl, ok := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer); ok {
		root = repl.ReplaceAll("{http.vars.root}", ".")
	}
	path := caddyhttp.SanitizedPathJoin(root, r.URL.Path)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}

	query := r.URL.RawQuery
	if !versionHash.MatchString(query) {
		return maxAge(s.UnversionedMaxAge, "must-revalidate")
	}
	hash, err := s.hashes.get(path, info)
	if err != nil || hash != query {
		return maxAge(s.MismatchMaxAge, "must-revalidate")
	}
	return maxAge(s.VersionedMaxAge, "immutable")
}

func maxAge(d caddy.Duration, directive string) string {
	seconds := int64(time.Duration(d).Seconds())
	return fmt.Sprintf("public, s-maxage=%d, max-age=%d, %s", seconds, seconds, directive)
}

type staticWriter struct {
	*caddyhttp.ResponseWriterWrapper
	s           *StaticConfig
	r           *http.Request
	wroteHeader bool
}

func (sw *staticWriter) WriteHeader(status int) {
	// 1xx responses such as 103 Early Hints come before the real one
	if sw.wroteHeader || status < 200 {
		sw.ResponseWriterWrapper.WriteHeader(status)
		return
	}
	sw.wroteHeader = true
	if status < 300 || status == http.StatusNotModified {
		if cc := sw.s.cacheControl(sw.r); cc != "" {
			sw.Header().Set("Cache-Control", cc)
		}
	}
	sw.ResponseWriterWrapper.WriteHeader(status)
}

func (sw *staticWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.WriteHeader(http.StatusOK)
	}
	return sw.ResponseWriterWrapper.Write(b)
}

// ReadFrom is where io.Copy lands, and the wrapper's own would skip WriteHeader.
func (sw *staticWriter) ReadFrom(src io.Reader) (int64, error) {
	if !sw.wroteHeader {
		sw.WriteHeader(http.StatusOK)
	}
	return sw.ResponseWriterWrapper.ReadFrom(src)
}

// hashCache keeps each file's hash until the file's size or time changes, so
// an icon asked for on every page render is read once per deploy. Only files
// that exist get an entry, so a crawler asking for made up paths adds nothing.
type hashCache struct {
	mu      sync.Mutex
	entries map[string]hashEntry
}

type hashEntry struct {
	modTime time.Time
	size    int64
	hash    string
}

func (c *hashCache) get(path string, info os.FileInfo) (string, error) {
	c.mu.Lock()
	e, ok := c.entries[path]
	c.mu.Unlock()
	if ok && e.size == info.Size() && e.modTime.Equal(info.ModTime()) {
		return e.hash, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	hash := hex.EncodeToString(h.Sum(nil))[:5]

	c.mu.Lock()
	c.entries[path] = hashEntry{modTime: info.ModTime(), size: info.Size(), hash: hash}
	c.mu.Unlock()
	return hash, nil
}

// unmarshalStatic reads the block after "static":
//
//	static {
//		paths <path...>
//		versioned_max_age <duration>
//		unversioned_max_age <duration>
//		mismatch_max_age <duration>
//	}
func unmarshalStatic(d *caddyfile.Dispenser) (*StaticConfig, error) {
	s := &StaticConfig{}
	if d.NextArg() {
		return nil, d.ArgErr()
	}
	for nesting := d.Nesting(); d.NextBlock(nesting); {
		if d.Val() == "paths" {
			s.Paths = append(s.Paths, d.RemainingArgs()...)
			if len(s.Paths) == 0 {
				return nil, d.ArgErr()
			}
			continue
		}
		var target *caddy.Duration
		switch d.Val() {
		case "versioned_max_age":
			target = &s.VersionedMaxAge
		case "unversioned_max_age":
			target = &s.UnversionedMaxAge
		case "mismatch_max_age":
			target = &s.MismatchMaxAge
		default:
			return nil, d.Errf("unknown static option %q", d.Val())
		}
		args := d.RemainingArgs()
		if len(args) != 1 {
			return nil, d.ArgErr()
		}
		v, err := caddy.ParseDuration(args[0])
		if err != nil {
			return nil, d.Errf("%s: %v", args[0], err)
		}
		*target = caddy.Duration(v)
	}
	return s, nil
}
