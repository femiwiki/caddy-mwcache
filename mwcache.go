package mwcache

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

// The backend is global so that the cache survives a config reload, and server
// blocks that serve the same host purge the same entries. It is created once and reused unless
// the requested backend options change.
var (
	backendMu      sync.Mutex
	backend        Backend
	backendName    string
	backendOptions map[string]string
	// backendUsers counts the handlers provisioned with backend, so a replaced
	// backend is closed once Caddy cleans up the last of them.
	backendUsers *int
)

// sharedBackend returns the backend for c and the function that the handler
// calls, once, when it is cleaned up.
func sharedBackend(c Config) (Backend, func(), error) {
	backendMu.Lock()
	defer backendMu.Unlock()
	if backend != nil && backendName == c.Backend && maps.Equal(backendOptions, c.RistrettoConfig) {
		return backend, acquireBackend(), nil
	}
	var b Backend
	switch c.Backend {
	case "":
		return nil, nil, errors.New("no backend")
	case "ristretto":
		rb, err := newRistrettoBackend(c.RistrettoConfig)
		if err != nil {
			return nil, nil, err
		}
		b = rb
	default:
		return nil, nil, fmt.Errorf("unknown backend: %s", c.Backend)
	}
	// Nothing holds a backend left without users, such as one built by a
	// config load that failed
	if backend != nil && *backendUsers == 0 {
		backend.close()
	}
	backend = b
	backendUsers = new(int)
	backendName = c.Backend
	backendOptions = maps.Clone(c.RistrettoConfig)
	return backend, acquireBackend(), nil
}

// acquireBackend counts one more user of the current backend and returns the
// function that gives it back. The caller holds backendMu.
func acquireBackend() func() {
	b, users := backend, backendUsers
	*users++
	var once sync.Once
	return func() {
		once.Do(func() {
			backendMu.Lock()
			defer backendMu.Unlock()
			*users--
			// The current backend stays open without users, so that the next
			// config load takes it over
			if *users == 0 && users != backendUsers {
				b.close()
			}
		})
	}
}

type metadata struct {
	Header http.Header
	Status int
}

var errStale = errors.New("stale")

var (
	uncacheableDirective = regexp.MustCompile(`(private|no-cache|no-store)`)
	sessionCookiePattern = regexp.MustCompile(`([sS]ession|Token)=`)
	sMaxAge              = regexp.MustCompile(`s-maxage\s*=\s*(\d+)`)
)

var bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective("mwcache", parseCaddyfile)
}

// Handler serves MediaWiki pages from the cache and fills it.
type Handler struct {
	logger  *zap.Logger
	backend Backend
	release func()
	// Config is exported so that it survives the trip through the adapted
	// JSON. A config that is loaded as JSON, by `caddy run --config caddy.json`
	// or by POSTing it to /load, never runs the Caddyfile adapter, so the
	// handler has to carry its own options.
	Config Config `json:"config"`
}

// Config holds the handler options.
type Config struct {
	Backend         string            `json:"backend,omitempty"`
	PurgeACL        []string          `json:"purge_acl,omitempty"`
	RistrettoConfig map[string]string `json:"ristretto_config,omitempty"`
	Static          *StaticConfig     `json:"static,omitempty"`
	// HostAliases maps a host to the one whose entries it reads, purges and
	// fills, such as 127.0.0.1, where MediaWiki sends its PURGEs, to the
	// public host.
	HostAliases map[string]string `json:"host_aliases,omitempty"`
}

// CaddyModule implements caddy.Module
func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.mwcache",
		New: func() caddy.Module { return new(Handler) },
	}
}

// CIDRContainsIP reports whether needleStr, with any port dropped, is cidr or
// falls in it.
func CIDRContainsIP(cidr string, needleStr string) bool {
	// Ignore port
	if strings.Contains(needleStr, ":") {
		needleStr = strings.Split(needleStr, ":")[0]
	}

	// Return correct value even if the given 'cidr' is a ip address other then a cidr'
	haystackIP := net.ParseIP(cidr)
	needleIP := net.ParseIP(needleStr)
	if haystackIP.Equal(needleIP) {
		return true
	}

	// Cidr check
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return ipNet.Contains(needleIP)
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if s := h.Config.Static; s != nil && s.matches(r) {
		return s.serve(w, r, next)
	}
	switch r.Method {
	case "PURGE":
		// Check Domain against purge acl
		// See https://github.com/wikimedia/puppet/blob/120dff45/modules/varnish/templates/wikimedia-frontend.vcl.erb#L501-L513
		acl := h.Config.PurgeACL
		found := false
		for _, cidr := range acl {
			if CIDRContainsIP(cidr, r.RemoteAddr) {
				found = true
				break
			}
		}

		if !found {
			h.logger.Info("purging from " + r.RemoteAddr + " is blocked")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte("Method not allowed"))
			return nil
		}
		key := h.createKey(r)
		h.backend.delete(key) //nolint:errcheck // the purge response is 204 whether or not the key was held
		h.logger.Debug("purged:  " + key)
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("Purged"))
		return nil
	case http.MethodHead:
		return h.serveUsingCacheIfAvaliable(w, r, next)
	case http.MethodGet:
		return h.serveUsingCacheIfAvaliable(w, r, next)
	default:
		return next.ServeHTTP(w, r)
	}
}

func (h *Handler) serveUsingCacheIfAvaliable(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if !requestIsCacheable(r) {
		h.logger.Debug("request is uncacheable: " + r.URL.RequestURI())
		return next.ServeHTTP(w, r)
	}
	key := h.createKey(r)
	val, err := h.backend.get(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			h.logger.Debug("cache miss: " + key)
			return h.fetch(key, w, r, next)
		}
		return err
	}
	// Cache hit, response with cache
	h.logger.Debug("cache hit: " + key)

	buf, _ := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)
	buf.WriteString(val)

	if err := h.writeResponse(w, r, buf, true); err != nil {
		if errors.Is(err, errStale) {
			h.logger.Debug("staled, drop: " + key)
			if err := h.fetch(key, w, r, next); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

// fetch serves a miss from upstream, or from the response of the miss for key
// already in flight, if that one may be cached.
func (h *Handler) fetch(key string, w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// A HEAD response has no body, and the key does not hold the method, so
	// storing it would hand an empty body to every GET after it.
	if r.Method == http.MethodHead {
		return next.ServeHTTP(w, r)
	}
	f, leader := misses.join(key)
	if f != nil {
		select {
		case <-f.done:
		case <-r.Context().Done():
			return r.Context().Err()
		}
		if f.entry != nil {
			h.logger.Debug("coalesced: " + key)
			return h.writeResponse(w, r, bytes.NewBuffer(f.entry), false)
		}
		h.logger.Debug("not coalesced, response is unshared: " + key)
	}
	if !leader {
		entry, err := h.serveAndCache(key, w, r, next)
		if entry != nil {
			misses.unpass(key)
		}
		return err
	}
	// Landed in a defer, so that a panic upstream does not leave the
	// waiters hanging
	var entry []byte
	failed := true
	defer func() {
		if n := misses.land(key, entry, failed); n > 0 {
			h.logger.Debug("woke "+strconv.Itoa(n)+" waiting: "+key, zap.Bool("shared", entry != nil))
		}
	}()
	entry, err := h.serveAndCache(key, w, r, next)
	failed = err != nil
	return err
}

// serveAndCache sends r upstream, stores the response if it may be cached and
// returns it as stored, or nil if it was not.
func (h *Handler) serveAndCache(key string, w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) ([]byte, error) {
	buf, _ := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	var meta metadata
	rec := caddyhttp.NewResponseRecorder(w, buf, func(status int, header http.Header) bool {
		// TODO research cache spec for MediaWiki
		if status < 200 || status >= 400 ||
			// https://github.com/femiwiki/caddy-mwcache/issues/16
			status == 304 {
			return false
		}
		c := header.Get("Cache-Control")
		if c == "" {
			return false
		}
		if uncacheableDirective.MatchString(c) {
			return false
		}
		if header.Get("Set-Cookie") != "" {
			return false
		}
		if header.Get("Date") == "" {
			header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
		}
		meta = metadata{Header: header.Clone(), Status: status}
		return true
	})

	// Fetch upstream response
	if err := next.ServeHTTP(rec, r); err != nil {
		return nil, err
	}
	if !rec.Buffered() {
		// The recorder streamed the response through, so there is nothing left to write
		h.logger.Debug("response is uncacheable: " + key)
		return nil, nil
	}

	entry, err := newEntry(meta, buf.Bytes())
	if err != nil {
		return nil, err
	}
	if err := h.backend.put(key, entry.String()); err != nil {
		h.logger.Warn("caching failed: "+key, zap.Error(err))
	} else {
		h.logger.Debug("put cache: " + key)
	}

	// Taken before writeResponse reads the buffer, and never written again,
	// so waiters can each read it at once
	stored := entry.Bytes()
	return stored, h.writeResponse(w, r, entry, false)
}

// newEntry stores the body compressed: an entry takes a fifth of the space,
// and a hit no longer pays the encode handler to compress it again.
func newEntry(meta metadata, body []byte) (*bytes.Buffer, error) {
	if meta.Header.Get("Content-Encoding") == "" && len(body) > 0 {
		compressed, err := gzipBytes(body)
		if err != nil {
			return nil, err
		}
		body = compressed
		meta.Header.Set("Content-Encoding", "gzip")
		meta.Header.Del("Content-Length")
		if !strings.Contains(strings.ToLower(meta.Header.Get("Vary")), "accept-encoding") {
			meta.Header.Add("Vary", "Accept-Encoding")
		}
	}
	entry := new(bytes.Buffer)
	if err := gob.NewEncoder(entry).Encode(meta); err != nil {
		return nil, err
	}
	entry.Write(body)
	return entry, nil
}

func gzipBytes(b []byte) ([]byte, error) {
	out := new(bytes.Buffer)
	zw := gzip.NewWriter(out)
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// acceptsGzip reports whether the client listed gzip, or *, in Accept-Encoding
// with a q above zero.
func acceptsGzip(r *http.Request) bool {
	for part := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(part, ";")
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "gzip" && name != "*" {
			continue
		}
		q, found := strings.CutPrefix(strings.TrimSpace(params), "q=")
		if !found {
			return true
		}
		v, err := strconv.ParseFloat(q, 64)
		return err == nil && v > 0
	}
	return false
}

func (h *Handler) writeResponse(w http.ResponseWriter, r *http.Request, buf *bytes.Buffer, fromCache bool) error {
	header := w.Header()

	var meta metadata
	if err := gob.NewDecoder(buf).Decode(&meta); err != nil {
		return err
	}
	if fromCache && !h.isFresh(meta.Header) {
		return errStale
	}
	if fromCache {
		setAge(meta.Header, time.Now())
	}
	if meta.Status == http.StatusOK && notModified(r, meta.Header) {
		writeNotModified(w, meta.Header)
		return nil
	}

	var body io.Reader = buf
	if meta.Header.Get("Content-Encoding") == "gzip" && !acceptsGzip(r) {
		// A HEAD writes no body, so it needs no reader.
		if r.Method != http.MethodHead {
			zr, err := gzip.NewReader(buf)
			if err != nil {
				return err
			}
			defer func() { _ = zr.Close() }()
			body = zr
		}
		meta.Header.Del("Content-Encoding")
		meta.Header.Del("Content-Length")
	}

	// Write header. The two that describe the body come only from the entry,
	// since on a miss the writer still holds what the upstream sent.
	header.Del("Content-Encoding")
	header.Del("Content-Length")
	maps.Copy(header, meta.Header)
	w.WriteHeader(meta.Status)

	// Write body
	if r.Method == http.MethodHead {
		return nil
	}
	if _, err := io.Copy(w, body); err != nil {
		return err
	}
	return nil
}

// setAge adds the Age a shared cache sends, counted from the stored Date.
// https://www.rfc-editor.org/rfc/rfc9111#section-5.1
func setAge(header http.Header, now time.Time) {
	date, err := http.ParseTime(header.Get("Date"))
	if err != nil {
		return
	}
	age := max(int64(now.Sub(date)/time.Second), 0)
	header.Set("Age", strconv.FormatInt(age, 10))
}

// notModified evaluates If-None-Match, or else If-Modified-Since, against a
// stored 200. https://www.rfc-editor.org/rfc/rfc9111#section-4.3.2
func notModified(r *http.Request, header http.Header) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		return etagMatches(inm, header.Get("ETag"))
	}
	ims, err := http.ParseTime(r.Header.Get("If-Modified-Since"))
	if err != nil {
		return false
	}
	lastModified := header.Get("Last-Modified")
	if lastModified == "" {
		lastModified = header.Get("Date")
	}
	modified, err := http.ParseTime(lastModified)
	if err != nil {
		return false
	}
	return !modified.After(ims)
}

// etagMatches reports whether any tag in an If-None-Match list matches etag
// by the weak comparison. A * matches any stored response.
func etagMatches(list, etag string) bool {
	if strings.TrimSpace(list) == "*" {
		return true
	}
	if etag == "" {
		return false
	}
	want := strings.TrimPrefix(etag, "W/")
	for tag := range strings.SplitSeq(list, ",") {
		if strings.TrimPrefix(strings.TrimSpace(tag), "W/") == want {
			return true
		}
	}
	return false
}

// writeNotModified sends a 304 with the stored header, less the fields that
// describe a body, as net/http's ServeContent does.
func writeNotModified(w http.ResponseWriter, stored http.Header) {
	header := w.Header()
	maps.Copy(header, stored)
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Encoding"} {
		header.Del(k)
	}
	w.WriteHeader(http.StatusNotModified)
}

// isFresh investments a request that has the given header is fresh.
// Targets only mediawiki-specific directives defined below files:
//   - https://github.com/wikimedia/mediawiki/blob/master/includes/OutputPage.php
//   - https://github.com/wikimedia/mediawiki/blob/master/includes/api/ApiMain.php
//   - https://github.com/wikimedia/mediawiki/blob/master/includes/AjaxResponse.php
func (h *Handler) isFresh(header http.Header) bool {
	var maxAgeInt uint64
	var err error
	var date time.Time

	cc := header.Get("Cache-Control")
	if cc == "" {
		// Cache-Control directive is not provided.
		h.logger.Debug("stored cache has no Cache-Control header")
		return true
	}
	submatch := sMaxAge.FindStringSubmatch(cc)
	if len(submatch) != 2 {
		h.logger.Debug("Cache-Control has no s-maxage")
		return true
	}
	maxAgeStr := submatch[1]
	if maxAgeInt, err = strconv.ParseUint(maxAgeStr, 10, 32); err != nil {
		h.logger.Debug("parsing " + maxAgeStr + " failed")
		return true
	}

	dateHeader := header.Get("Date")
	if dateHeader == "" {
		h.logger.Debug("Date header is missing")
		return true
	}

	date, err = http.ParseTime(dateHeader)
	if err != nil {
		h.logger.Debug("parsing " + dateHeader + " failed")
		return true
	}
	date = date.UTC()
	now := time.Now().UTC()

	maxAge := time.Duration(maxAgeInt) * time.Second
	return date.Add(maxAge).After(now)
}

// createKey puts the host before the path, as Wikimedia's Varnish hashes the
// Host along with the URL, so two sites behind one handler never share an
// entry. The scheme is left out: MediaWiki purges over http what it serves
// over https. A host named in HostAliases uses the key of the host it stands
// for.
func (h *Handler) createKey(r *http.Request) string {
	host := normalizeHost(r.Host)
	if canonical, ok := h.Config.HostAliases[host]; ok {
		host = canonical
	}
	return host + r.URL.RequestURI()
}

// normalizeHost lowercases a Host header and drops its port and any trailing
// dot, so the spellings of one host share a key.
func normalizeHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

// NOTE: requests to RESTBase is not reach this module because of reverse_proxy has higher order
func requestIsCacheable(r *http.Request) bool {
	// Any Authorization header, an OAuth token too, can log a request in
	// without a cookie, so it passes the cache as in Wikimedia's Varnish.
	if _, ok := r.Header["Authorization"]; ok {
		return false
	}
	// HTTP/2 does not check :authority, and a host with a slash in it would
	// split the key at a different place, such as a/b.femiwiki.com/w/X
	// against host a and path /b.femiwiki.com/w/X
	if strings.Contains(r.Host, "/") {
		return false
	}
	if r.URL.Path == loadPHPPath {
		// load.php defines MW_NO_SESSION, so the session cookie cannot change
		// what it sends, and a logged-in request shares the anonymous entry.
		// Only the user= modules are a user's own.
		if hasUserParam(r.URL.RawQuery) {
			return false
		}
	} else if hasSessionCookie(r) {
		return false
	}
	return true
}

// loadPHPPath is ResourceLoader's entry point with $wgScriptPath set to "".
// Only this exact path is load.php; with $wgArticlePath = "/w/$1",
// /w/load.php is a wiki page.
const loadPHPPath = "/load.php"

// hasUserParam reports whether any query key mentions user, such as user=,
// user[]= or " user=". PHP trims and rewrites key names before ResourceLoader
// reads them, so this matches loosely; no other key load.php reads has user
// in its name. It splits on ; as well, in case arg_separator.input has it.
// A key Go cannot decode counts as user, since PHP decodes it leniently.
func hasUserParam(rawQuery string) bool {
	for _, pair := range strings.FieldsFunc(rawQuery, func(c rune) bool { return c == '&' || c == ';' }) {
		key, _, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(key)
		if err != nil || strings.Contains(strings.ToLower(key), "user") {
			return true
		}
	}
	return false
}

// hasSessionCookie reports whether the request carries a session or token
// cookie, the way Wikimedia's Varnish tells a logged-in request apart. It then
// lets such requests share the anonymous entry unless the response varies on
// Cookie.
// https://github.com/wikimedia/operations-puppet/blob/ecfe533f59092e7728cac31873de9b022e9e72d5/modules/varnish/templates/text-frontend.inc.vcl.erb#L366-L389
// https://github.com/wikimedia/operations-puppet/blob/ecfe533f59092e7728cac31873de9b022e9e72d5/modules/varnish/templates/text-frontend.inc.vcl.erb#L811-L828
func hasSessionCookie(r *http.Request) bool {
	cookie := r.Header.Get("Cookie")
	return sessionCookiePattern.MatchString(cookie)
}

// Interface guards
var (
	_ caddy.Module                = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.CleanerUpper          = (*Handler)(nil)
	_ caddy.Validator             = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
)
