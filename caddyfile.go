package mwcache

import (
	"fmt"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	h.Config = Config{
		Backend:  "ristretto",
		PurgeAcl: []string{"127.0.0.1"},
	}
	c := &h.Config
	for d.Next() {
		if len(d.RemainingArgs()) == 1 {
			switch d.Val() {
			case "ristretto":
				// Use default
			default:
				return d.ArgErr()
			}
		}

		for d.NextBlock(0) {
			switch d.Val() {
			case "ristretto":
				// Use default backend
				// Unmarshal block
				if len(d.RemainingArgs()) != 1 {
					c.RistrettoConfig = map[string]string{}
					for d.NextBlock(1) {
						k := d.Val()
						if !d.Next() {
							return d.ArgErr()
						}
						c.RistrettoConfig[k] = d.Val()
					}
				}
			case "purge_acl":
				// TODO throw error when an empty block is given
				c.PurgeAcl = nil
				if len(d.RemainingArgs()) == 1 && !d.NextBlock(1) {
					c.PurgeAcl = []string{d.Val()}
				} else {
					for d.NextBlock(1) {
						c.PurgeAcl = append(c.PurgeAcl, d.Val())
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
	if h.Config.Backend == "" {
		return fmt.Errorf("no backend")
	}
	if h.Config.PurgeAcl == nil {
		return fmt.Errorf("no purge acl")
	}
	if h.Config.Backend == "ristretto" {
		if err := ValidateRistrettoConfig(h.Config.RistrettoConfig); err != nil {
			return err
		}
	}
	return nil
}

// Provision implements caddy.Provisioner.
func (h *Handler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger(h)
	h.logger.Info("logger is created")
	b, err := sharedBackend(h.Config)
	if err != nil {
		return err
	}
	h.backend = b
	return nil
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m Handler
	err := m.UnmarshalCaddyfile(h.Dispenser)
	return m, err
}

// Interface guards
var (
	_ caddyfile.Unmarshaler = (*Handler)(nil)
)
