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
				if err := unmarshalRistretto(d, c); err != nil {
					return err
				}
			case "static":
				s, err := unmarshalStatic(d)
				if err != nil {
					return err
				}
				c.Static = s
			case "host_alias":
				if err := unmarshalHostAlias(d, c); err != nil {
					return err
				}
			case "purge_acl":
				unmarshalPurgeAcl(d, c)
			default:
				return d.ArgErr()
			}
		}
	}
	return nil
}

func unmarshalPurgeAcl(d *caddyfile.Dispenser, c *Config) {
	// TODO throw error when an empty block is given
	c.PurgeAcl = nil
	if len(d.RemainingArgs()) == 1 && !d.NextBlock(1) {
		c.PurgeAcl = []string{d.Val()}
	} else {
		for d.NextBlock(1) {
			c.PurgeAcl = append(c.PurgeAcl, d.Val())
		}
	}
}

// unmarshalHostAlias reads `host_alias <host> <alias>...`, which may be given
// more than once.
func unmarshalHostAlias(d *caddyfile.Dispenser, c *Config) error {
	args := d.RemainingArgs()
	if len(args) < 2 {
		return d.ArgErr()
	}
	if c.HostAliases == nil {
		c.HostAliases = map[string]string{}
	}
	canonical := normalizeHost(args[0])
	for _, a := range args[1:] {
		alias := normalizeHost(a)
		if _, ok := c.HostAliases[alias]; ok {
			return d.Errf("%s is already an alias", a)
		}
		c.HostAliases[alias] = canonical
	}
	return nil
}

func unmarshalRistretto(d *caddyfile.Dispenser, c *Config) error {
	if len(d.RemainingArgs()) == 1 {
		return nil
	}
	c.RistrettoConfig = map[string]string{}
	for d.NextBlock(1) {
		k := d.Val()
		if !d.Next() {
			return d.ArgErr()
		}
		c.RistrettoConfig[k] = d.Val()
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
	for alias, canonical := range h.Config.HostAliases {
		if alias == "" || canonical == "" {
			return fmt.Errorf("host_alias: a host cannot be empty")
		}
		if alias == canonical {
			return fmt.Errorf("host_alias: %s is an alias of itself", alias)
		}
		if _, ok := h.Config.HostAliases[canonical]; ok {
			return fmt.Errorf("host_alias: %s is an alias of %s, which is an alias itself", alias, canonical)
		}
	}
	if h.Config.Static != nil {
		return h.Config.Static.validate()
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
	// A config loaded as JSON never ran the Caddyfile adapter, which
	// normalizes the hosts as it reads them
	if len(h.Config.HostAliases) > 0 {
		aliases := make(map[string]string, len(h.Config.HostAliases))
		for alias, canonical := range h.Config.HostAliases {
			a, c := normalizeHost(alias), normalizeHost(canonical)
			// Two spellings of one alias would otherwise leave the winner to map order
			if prev, ok := aliases[a]; ok && prev != c {
				return fmt.Errorf("host_alias: %s is an alias of both %s and %s", a, prev, c)
			}
			aliases[a] = c
		}
		h.Config.HostAliases = aliases
	}
	if h.Config.Static != nil {
		if err := h.Config.Static.provision(ctx); err != nil {
			return err
		}
	}
	registerMetrics(ctx.GetMetricsRegistry(), h.logger)
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
