package mwcache

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
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
)

func sharedBackend(c Config) (Backend, error) {
	backendMu.Lock()
	defer backendMu.Unlock()
	if backend != nil && backendName == c.Backend && maps.Equal(backendOptions, c.RistrettoConfig) {
		return backend, nil
	}
	switch c.Backend {
	case "":
		return nil, fmt.Errorf("no backend")
	case "ristretto":
		b, err := newRistrettoBackend(c.RistrettoConfig)
		if err != nil {
			return nil, err
		}
		backend = b
	default:
		return nil, fmt.Errorf("unknown backend: %s", c.Backend)
	}
	backendName = c.Backend
	backendOptions = maps.Clone(c.RistrettoConfig)
	return backend, nil
}

type metadata struct {
	Header http.Header
	Status int
}

var errStale = fmt.Errorf("stale")

const timeFormat = "Mon, 2 Jan 2006 15:04:05 MST"

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective("mwcache", parseCaddyfile)
}

type Handler struct {
	logger  *zap.Logger
	backend Backend
	// Config is exported so that it survives the trip through the adapted
	// JSON. A config that is loaded as JSON, by `caddy run --config caddy.json`
	// or by POSTing it to /load, never runs the Caddyfile adapter, so the
	// handler has to carry its own options.
	Config Config `json:"config"`
}

type Config struct {
	Backend         string            `json:"backend,omitempty"`
	PurgeAcl        []string          `json:"purge_acl,omitempty"`
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

func CIDRContainsIP(cidr string, needleStr string) bool {
	// Ignore port
	if strings.Contains(needleStr, ":") {
		needleStr = strings.Split(needleStr, ":")[0]
	}

	// Return correct value even if the given 'cidr' is a ip address other then a cidr'
	haystackIP := net.ParseIP(cidr)
	needleIp := net.ParseIP(needleStr)
	if haystackIP.Equal(needleIp) {
		return true
	}

	// Cidr check
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return ipNet.Contains(needleIp)
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if s := h.Config.Static; s != nil && s.matches(r) {
		return s.serve(w, r, next)
	}
	switch r.Method {
	case "PURGE":
		// Check Domain against purge acl
		// See https://github.com/wikimedia/puppet/blob/120dff45/modules/varnish/templates/wikimedia-frontend.vcl.erb#L501-L513
		acl := h.Config.PurgeAcl
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
		h.logger.Info("purged:  " + key)
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

func (h Handler) serveUsingCacheIfAvaliable(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if !requestIsCacheable(r) {
		h.logger.Info("request is uncacheable: " + r.URL.RequestURI())
		return next.ServeHTTP(w, r)
	}
	key := h.createKey(r)
	val, err := h.backend.get(key)
	if err != nil {
		if err == ErrKeyNotFound {
			h.logger.Info("cache miss: " + key)
			if err := h.serveAndCache(key, w, r, next); err != nil {
				return err
			}
			return nil
		}
		return err
	}
	// Cache hit, response with cache
	h.logger.Info("cache hit: " + key)

	pool := sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
	buf := pool.Get().(*bytes.Buffer)
	buf.Reset()
	defer pool.Put(buf)
	buf.Write([]byte(val))

	if err := h.writeResponse(w, r, buf, true); err != nil {
		if err == errStale {
			h.logger.Info("staled, drop: " + key)
			if err := h.serveAndCache(key, w, r, next); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (h Handler) serveAndCache(key string, w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// A HEAD response has no body, and the key does not hold the method, so
	// storing it would hand an empty body to every GET after it.
	if r.Method == http.MethodHead {
		return next.ServeHTTP(w, r)
	}
	pool := sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
	buf := pool.Get().(*bytes.Buffer)
	buf.Reset()
	defer pool.Put(buf)

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
		if match, err := regexp.Match(`(private|no-cache|no-store)`, []byte(c)); err == nil && match {
			return false
		}
		if header.Get("Set-Cookie") != "" {
			return false
		}
		if header.Get("Date") == "" {
			header.Set("Date", time.Now().UTC().Format(timeFormat))
		}
		meta = metadata{Header: header.Clone(), Status: status}
		return true
	})

	// Fetch upstream response
	if err := next.ServeHTTP(rec, r); err != nil {
		return err
	}
	if !rec.Buffered() {
		// The recorder streamed the response through, so there is nothing left to write
		h.logger.Info("response is uncacheable: " + key)
		return nil
	}

	entry, err := newEntry(meta, buf.Bytes())
	if err != nil {
		return err
	}
	if err := h.backend.put(key, entry.String()); err != nil {
		return err
	}
	h.logger.Info("put cache: " + key)

	return h.writeResponse(w, r, entry, false)
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
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
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

func (h Handler) writeResponse(w http.ResponseWriter, r *http.Request, buf *bytes.Buffer, fromCache bool) error {
	header := w.Header()

	var meta metadata
	if err := gob.NewDecoder(buf).Decode(&meta); err != nil {
		return err
	}
	if fromCache && !h.isFresh(meta.Header) {
		return errStale
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
	for k, v := range meta.Header {
		header[k] = v
	}
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

// isFresh investments a request that has the given header is fresh.
// Targets only mediawiki-specific directives defined below files:
//   - https://github.com/wikimedia/mediawiki/blob/master/includes/OutputPage.php
//   - https://github.com/wikimedia/mediawiki/blob/master/includes/api/ApiMain.php
//   - https://github.com/wikimedia/mediawiki/blob/master/includes/AjaxResponse.php
func (h Handler) isFresh(header http.Header) bool {
	var maxAgeInt uint64
	var err error
	var date time.Time

	cc := header.Get("Cache-Control")
	if cc == "" {
		// Cache-Control directive is not provided.
		h.logger.Info("stored cache has no Cache-Control header")
		return true
	}
	re := regexp.MustCompile(`s-maxage\s*=\s*(\d+)`)
	submatch := re.FindStringSubmatch(cc)
	if len(submatch) != 2 {
		h.logger.Info("Cache-Control has no s-maxage")
		return true
	}
	maxAgeStr := submatch[1]
	if maxAgeInt, err = strconv.ParseUint(maxAgeStr, 10, 32); err != nil {
		h.logger.Info("parsing " + maxAgeStr + " failed")
		return true
	}

	dateHeader := header.Get("Date")
	if dateHeader == "" {
		h.logger.Info("Date header is missing")
		return true
	}

	date, err = time.Parse(timeFormat, dateHeader)
	if err != nil {
		h.logger.Info("parsing " + dateHeader + " failed")
		return true
	}
	date = date.UTC()
	now := time.Now().UTC()

	maxAge := time.Duration(maxAgeInt)
	return (date.Add(time.Second * maxAge)).After(now)
}

// createKey puts the host before the path, as Wikimedia's Varnish hashes the
// Host along with the URL, so two sites behind one handler never share an
// entry. The scheme is left out: MediaWiki purges over http what it serves
// over https. A host named in HostAliases uses the key of the host it stands
// for.
func (h Handler) createKey(r *http.Request) string {
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
	// don't cache authorized requests
	if _, _, ok := r.BasicAuth(); ok {
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
	match, err := regexp.Match(`([sS]ession|Token)=`, []byte(cookie))
	return err == nil && match
}

// Interface guards
var (
	_ caddy.Module                = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.Validator             = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
)
