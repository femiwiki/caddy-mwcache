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
		// TODO
		// See https://github.com/wikimedia/puppet/blob/120dff45/modules/varnish/templates/wikimedia-frontend.vcl.erb#L501-L513
		return nil
	case http.MethodGet:
		if !requestIsCacheable(r) {
			return next.ServeHTTP(w, r)
		}
		// TODO cache
		key := createKey(r)
		val, err := backend.get(key)
		if err != nil {
			if err == ErrKeyNotFound {
				// TODO serve and cache
				return nil
			}
			return err
		}
		pool := sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		}
		buf := pool.Get().(*bytes.Buffer)
		buf.Reset()
		buf.Write([]byte(val))
		io.Copy(w, buf)
		return nil
	default:
		return next.ServeHTTP(w, r)
	}
}

func createKey(r *http.Request) string {
	// TODO
	return ""
}

func requestIsCacheable(r *http.Request) bool {
	// don't cache authorized requests
	if _, _, ok := r.BasicAuth(); ok {
		return false
	}
	// TODO don't cache requests have query
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
