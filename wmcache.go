package wmcache

import "github.com/caddyserver/caddy/v2"

func init() {
	caddy.RegisterModule(WMCache{})
}

type WMCache struct {
}

func (WMCache) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.wmcache",
		New: func() caddy.Module { return new(WMCache) },
	}
}
