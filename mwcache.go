package mwcache

import "github.com/caddyserver/caddy/v2"

func init() {
	caddy.RegisterModule(MWCache{})
}

type MWCache struct {
}

func (MWCache) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.mwcache",
		New: func() caddy.Module { return new(MWCache) },
	}
}
