package mwcache

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

// backend and config are global to preserve the cache between reloads
var (
	backend Backend
	config  *Config
)

type metadata struct {
	Header http.Header
	Status int
}

var errUncacheable = fmt.Errorf("uncacheable")
var errStale = fmt.Errorf("stale")

const timeFormat = "Mon, 2 Jan 2006 15:04:05 MST"

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective("mwcache", parseCaddyfile)
}

type Handler struct {
	logger *zap.Logger
	config Config
}

type Config struct {
	Backend  string   `json:"backend,omitempty"`
	PurgeAcl []string `json:"purge_acl,omitempty"`
}

// CaddyModule implements caddy.Module
func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.mwcache",
		New: func() caddy.Module { return new(Handler) },
	}
}

// Provision implements caddy.Provisioner.
func (h *Handler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger(h)
	h.logger.Info("logger is created")
	switch config.Backend {
	case "map":
		backend = newMapBackend()
	case "badger":
		backend = &BadgerBackend{}
	}
	return nil
}

func CIDRContainsIP(cidr string, needleIP string) bool {
	if strings.Contains(needleIP, ":") {
		needleIP = strings.Split(needleIP, ":")[0]
	}

	haystickIP, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	if haystickIP != nil && haystickIP.Equal(net.ParseIP(needleIP)) {
		return true
	}
	return ipNet.Contains(net.ParseIP(needleIP))
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	switch r.Method {
	case "PURGE":
		// Check Domain against purge acl
		// See https://github.com/wikimedia/puppet/blob/120dff45/modules/varnish/templates/wikimedia-frontend.vcl.erb#L501-L513
		acl := config.PurgeAcl
		found := false
		for _, cidr := range acl {
			if CIDRContainsIP(cidr, r.RemoteAddr) {
				found = true
				break
			}
		}

		if !found {
			w.WriteHeader(http.StatusNoContent)
			w.Write([]byte("Domain not cached here"))
			return nil
		}
		key := createKey(r)
		backend.delete(key)
		h.logger.Info("purged for key " + key)
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
		return next.ServeHTTP(w, r)
	}
	key := createKey(r)
	val, err := backend.get(key)
	if err != nil {
		if err == ErrKeyNotFound {
			h.logger.Info("no hit for " + key)
			if err := h.serveAndCache(key, w, r, next); err != nil {
				return err
			}
			return nil
		}
		return err
	}
	// Cache hit, response with cache
	h.logger.Info("cache hit for " + key)

	pool := sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
	buf := pool.Get().(*bytes.Buffer)
	buf.Reset()
	defer pool.Put(buf)
	buf.Write([]byte(val))

	if err := h.writeResponse(w, buf, true); err != nil {
		if err == errStale {
			h.logger.Info("cache for " + key + " is stale, drop")
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
	pool := sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
	buf := pool.Get().(*bytes.Buffer)
	buf.Reset()
	defer pool.Put(buf)

	rec := caddyhttp.NewResponseRecorder(w, buf, func(status int, header http.Header) bool {
		// TODO research cache spec for MediaWiki
		if status < 200 || status >= 300 {
			return false
		}
		c := header.Get("Cache-Control")
		if match, err := regexp.Match(`(private|no-cache|no-store)`, []byte(c)); err == nil && match {
			return false
		}
		if header.Get("Set-Cookie") != "" {
			return false
		}
		if header.Get("Date") == "" {
			header.Set("Date", time.Now().UTC().Format(timeFormat))
		}
		// Recode header to buf
		err := gob.NewEncoder(buf).Encode(metadata{
			Header: header,
			Status: status,
		})
		if err != nil {
			h.logger.Error("", zap.Error(err))
			return false
		}

		// Body is recoded implicitly by the recoder
		return true
	})

	// Fetch upstream response
	if err := next.ServeHTTP(rec, r); err != nil {
		return err
	}
	if !rec.Buffered() || buf.Len() == 0 {
		return errUncacheable
	}

	// Cache recoded buf to the backend
	h.logger.Info("put cache for " + key)
	response := string(buf.Bytes())
	if err := backend.put(key, response); err != nil {
		return err
	}

	return h.writeResponse(w, buf, false)
}

func (h Handler) writeResponse(w http.ResponseWriter, buf *bytes.Buffer, fromCache bool) error {
	header := w.Header()

	var meta metadata
	if err := gob.NewDecoder(buf).Decode(&meta); err != nil {
		return err
	}
	if fromCache && !h.isFresh(meta.Header) {
		return errStale
	}

	// Write header
	for k, v := range meta.Header {
		header[k] = v
	}
	w.WriteHeader(meta.Status)

	// Write body
	if _, err := io.Copy(w, buf); err != nil {
		return err
	}
	return nil
}

// isFresh investments a request that has the given header is fresh.
// Targets only mediawiki-specific directives defined below files:
//	- https://github.com/wikimedia/mediawiki/blob/master/includes/OutputPage.php
//	- https://github.com/wikimedia/mediawiki/blob/master/includes/api/ApiMain.php
//	- https://github.com/wikimedia/mediawiki/blob/master/includes/AjaxResponse.php
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

func createKey(r *http.Request) string {
	// TODO use hash function?
	// Use URL.RequestURI() instead of URL.String() to truncate domain.
	return r.URL.RequestURI()
}

// NOTE: requests to RESTBase is not reach this module because of reverse_proxy has higher order
func requestIsCacheable(r *http.Request) bool {
	// don't cache authorized requests
	if _, _, ok := r.BasicAuth(); ok {
		return false
	}
	// don't cache request with session or token cookie
	// https://www.mediawiki.org/wiki/Manual:Varnish_caching#Configuring_Varnish
	cookie := r.Header.Get("Cookie")
	if match, err := regexp.Match(`([sS]ession|Token)=`, []byte(cookie)); err == nil && match {
		return false
	}
	if key := createKey(r); key == "" {
		return false
	}
	return true
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler. Syntax:
//
//	mwcache [<backend>]
//
//	mwcache {
//		[<backend>]
//		[purge_acl <purge_acl_address>]
//		[purge_acl {
//			<purge_acl_address>
//			[<purge_acl_address_2>]
//		}]
//	}
//
func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	h.config = Config{
		Backend:  "badger",
		PurgeAcl: []string{"127.0.0.1"},
	}
	config = &h.config
	for d.Next() {
		if len(d.RemainingArgs()) == 1 {
			switch d.Val() {
			case "map":
				config.Backend = d.Val()
			case "badger":
				// Use default
			default:
				return d.ArgErr()
			}
		}

		for d.NextBlock(0) {
			switch d.Val() {
			case "map":
				config.Backend = d.Val()
			case "badger":
				// Use default
			case "purge_acl":
				// TODO throw error when an empty block is given
				config.PurgeAcl = nil
				if len(d.RemainingArgs()) == 1 && !d.NextBlock(1) {
					config.PurgeAcl = []string{d.Val()}
				} else {
					for d.NextBlock(1) {
						config.PurgeAcl = append(config.PurgeAcl, d.Val())
					}
				}
			default:
				return d.ArgErr()
			}
		}
	}
	return nil
}

// Validate implements caddy.Validator.
func (h *Handler) Validate() error {
	h.config = *config
	if config.Backend == "" {
		return fmt.Errorf("no backend")
	}
	if config.PurgeAcl == nil {
		return fmt.Errorf("no purge acl")
	}
	return nil
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m Handler
	err := m.UnmarshalCaddyfile(h.Dispenser)
	return m, err
}

// Interface guards
var (
	_ caddy.Module                = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.Validator             = (*Handler)(nil)
	_ caddyfile.Unmarshaler       = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
)
