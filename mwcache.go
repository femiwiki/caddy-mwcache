package mwcache

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

// backend and config are global to preserve the cache between reloads
var (
	backend Backend
	config  Config
)

type metadata struct {
	Header http.Header
	Status int
}

var errUncacheable = fmt.Errorf("uncacheable")

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective("mwcache", parseCaddyfile)
}

type Handler struct {
	logger *zap.Logger
}

type Config struct {
	Backend string `json:"backend,omitempty"`
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

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	switch r.Method {
	case "PURGE":
		// TODO check ip against purge acl
		// See https://github.com/wikimedia/puppet/blob/120dff45/modules/varnish/templates/wikimedia-frontend.vcl.erb#L501-L513
		key := createKey(r)
		backend.delete(key)
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

	return h.writeResponse(w, buf)
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

	return h.writeResponse(w, buf)
}

func (h Handler) writeResponse(w http.ResponseWriter, buf *bytes.Buffer) error {
	// Write header
	var meta metadata
	if err := gob.NewDecoder(buf).Decode(&meta); err != nil {
		return err
	}
	header := w.Header()
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

func createKey(r *http.Request) string {
	// TODO use hash function?
	// Fragment is not used
	var key string
	if r.URL.Scheme != "" {
		key += r.URL.Scheme + "://"
	}
	if r.URL.Host != "" {
		key += r.URL.Host
	}
	if r.URL.Path != "" {
		key += r.URL.Path
	}
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	return key
}

// NOTE: requests to RESTBase is not reach this module because of reverse_proxy has higher order
func requestIsCacheable(r *http.Request) bool {
	// don't cache authorized requests
	if _, _, ok := r.BasicAuth(); ok {
		return false
	}
	// TODO check Cookie for session or token
	// TODO check Cache-Control header
	if key := createKey(r); key == "" {
		return false
	}
	return true
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler. Syntax:
//
//     mwcache [<backend>]
//
// TODO: Add purge_acl directive
func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	if config == (Config{}) {
		config = Config{}
	}
	for d.Next() {
		args := d.RemainingArgs()
		switch len(args) {
		case 0:
			config.Backend = "badger"
		case 1:
			switch args[0] {
			case "map":
			case "badger":
			default:
				return d.ArgErr()
			}
			config.Backend = args[0]
		default:
			return d.ArgErr()
		}
	}
	return nil
}

// Validate implements caddy.Validator.
func (h *Handler) Validate() error {
	if config.Backend == "" {
		return fmt.Errorf("no backend")
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
