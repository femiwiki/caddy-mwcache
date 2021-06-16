package mwcache

import (
	"fmt"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	h.config = Config{
		Backend:  "ristretto",
		PurgeAcl: []string{"127.0.0.1"},
	}
	config = &h.config
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
					config.RistrettoConfig = map[string]string{}
					for d.NextBlock(1) {
						k := d.Val()
						if !d.Next() {
							return d.ArgErr()
						}
						config.RistrettoConfig[k] = d.Val()
					}
				}
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
	if config.RistrettoConfig != nil {
		if err := ValidateRistrettoConfig(config.RistrettoConfig); err != nil {
			return err
		}
	}
	return nil
}

// Provision implements caddy.Provisioner.
func (h *Handler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger(h)
	h.logger.Info("logger is created")
	switch config.Backend {
	case "ristretto":
		b, err := newRistrettoBackend(config.RistrettoConfig)
		if err != nil {
			return err
		}
		backend = b
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
	_ caddyfile.Unmarshaler = (*Handler)(nil)
)
