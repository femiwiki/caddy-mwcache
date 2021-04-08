package mwcache

import (
	"fmt"
	"net/http"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(MWCache{})
	httpcaddyfile.RegisterHandlerDirective("mwcache", parseCaddyfile)
}

type MWCache struct {
	Backend      string `json:"backend,omitempty"`
	MemcachedUrl string `json:"memcached_url,omitempty"`

	logger *zap.Logger
}

func (MWCache) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.mwcache",
		New: func() caddy.Module { return new(MWCache) },
	}
}

func (c *MWCache) Provision(ctx caddy.Context) error {
	c.logger = ctx.Logger(c)
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (c MWCache) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(w, r)
}

// Validate implements caddy.Validator.
func (c *MWCache) Validate() error {
	if c.Backend == "" {
		return fmt.Errorf("no backend")
	}
	return nil
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler. Syntax:
//
//     mwcache [<backend>] [<memcached_url>]
//
func (c *MWCache) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		args := d.RemainingArgs()
		switch len(args) {
		case 0:
			c.config.Backend = "badger"
		case 1:
			switch args[0] {
			case "map":
			case "badger":
			default:
				return d.ArgErr()
			}
			c.config.Backend = args[0]
		default:
			return d.ArgErr()
		}
	}
	return nil
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m MWCache
	err := m.UnmarshalCaddyfile(h.Dispenser)
	return m, err
}

// Interface guards
var (
	_ caddy.Validator             = (*MWCache)(nil)
	_ caddy.Provisioner           = (*MWCache)(nil)
	_ caddyhttp.MiddlewareHandler = (*MWCache)(nil)
	_ caddyfile.Unmarshaler       = (*MWCache)(nil)
)
