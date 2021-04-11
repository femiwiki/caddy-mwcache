package mwcache

import (
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func TestDirectives(t *testing.T) {
	testcases := []struct {
		caddyfile string
		valid     bool
		backend   string
		acl       []string
	}{
		{
			caddyfile: `mwcache`,
			valid:     true,
			backend:   "badger",
			acl:       []string{"127.0.0.1"},
		},
		{
			caddyfile: `mwcache map`,
			valid:     true,
			backend:   "map",
			acl:       []string{"127.0.0.1"},
		},
		{
			caddyfile: `mwcache badger`,
			valid:     true,
			backend:   "badger",
			acl:       []string{"127.0.0.1"},
		},
		{
			caddyfile: `mwcache foo`,
			valid:     false,
			backend:   "",
			acl:       nil,
		},
		{
			// 4
			caddyfile: `
			mwcache {
				purge_acl 11.11.11.11
			}
			`,
			valid:   true,
			backend: "badger",
			acl:     []string{"11.11.11.11"},
		},
		{
			// 5
			caddyfile: `
			mwcache {
				map
				purge_acl 11.11.11.11
			}
			`,
			valid:   true,
			backend: "map",
			acl:     []string{"11.11.11.11"},
		},
		{
			caddyfile: `
			mwcache {
				purge_acl {
					11.11.11.11
					11.11.11.12
				}
			}
			`,
			valid:   true,
			backend: "badger",
			acl:     []string{"11.11.11.11", "11.11.11.12"},
		},
		{
			caddyfile: `
			mwcache {
				purge_acl {
					11.11.11.11
					11.11.11.12
					11.11.11.13
					11.11.11.14
				}
			}
			`,
			valid:   true,
			backend: "badger",
			acl:     []string{"11.11.11.11", "11.11.11.12", "11.11.11.13", "11.11.11.14"},
		},
		// TODO
		// {
		// 	caddyfile: `mwcache {
		// 		purge_acl
		// 	}
		// 	`,
		// 	valid:   false,
		// 	backend: "",
		// 	acl:     nil,
		// },
	}

	for i, test := range testcases {
		d := caddyfile.NewTestDispenser(test.caddyfile)
		m := &Handler{}
		err := m.UnmarshalCaddyfile(d)
		if test.valid && err != nil {
			t.Errorf("Test %d: error = %v", i, err)
		}
		if !test.valid && err == nil {
			t.Errorf("Test %d: Error should be thrown", i)
		}

		if !test.valid {
			continue
		}
		if m.config.Backend != test.backend {
			t.Errorf("Test %d: Expected: '%s' but got '%s'", i, test.backend, m.config.Backend)
		}

		// TODO Compare all elements
		if len(m.config.PurgeAcl) != len(test.acl) || m.config.PurgeAcl[0] != test.acl[0] {
			e := strings.Join(test.acl, ", ")
			a := strings.Join(m.config.PurgeAcl, ", ")
			t.Errorf("Test %d: Expected: '%s' but got '%s'", i, e, a)
		}
	}
}
