package mwcache

import (
	"bytes"
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

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective("mwcache", parseCaddyfile)
}

type Handler struct {
	logger *zap.Logger
}

type Config struct {
	Backend string `json:"backend,omitempty"`
	// MemcachedUrl string `json:"memcached_url,omitempty"`
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
		// TODO check ip
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
	_, err = io.Copy(w, buf)
	if err != nil {
		return err
	}
	return nil
}

func (h Handler) serveAndCache(key string, w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Fetch upstream response
	pool := sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
	buf := pool.Get().(*bytes.Buffer)
	buf.Reset()
	defer pool.Put(buf)
	rec := caddyhttp.NewResponseRecorder(w, buf, func(status int, header http.Header) bool {
		return true
	})
	next.ServeHTTP(rec, r)
	res := string(buf.Bytes())
	// Cache
	if err := backend.put(key, res); err != nil {
		return err
	}
	h.logger.Info("put cache for " + key)
	return rec.WriteResponse()
}

func createKey(r *http.Request) string {
	fmt.Println(r.URL.String())
	// Do not use Fragment
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
	if key := createKey(r); key == "" {
		return false
	}
	return true
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler. Syntax:
//
//     mwcache [<backend>]
//
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
