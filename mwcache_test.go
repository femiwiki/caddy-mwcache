package mwcache

import (
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

func TestDirectives(t *testing.T) {
	h := httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(`
		localhost:2015 {
			mwcache
		}`),
	}
	_, err := parseCaddyfile(h)
	if err != nil {
		t.Errorf("error = %v", err)
	}

	h = httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(`
		localhost:2015 {
			mwcache foo
		}`),
	}
	_, err = parseCaddyfile(h)
	if err == nil {
		t.Errorf("Error should be thrown")
	}

}
