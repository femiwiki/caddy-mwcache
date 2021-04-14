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
		badger    map[string]string
		ristretto map[string]string
	}{
		{
			caddyfile: `mwcache`,
			valid:     true,
			backend:   "ristretto",
			acl:       []string{"127.0.0.1"},
			badger:    nil,
			ristretto: nil,
		},
		{
			caddyfile: `mwcache map`,
			valid:     true,
			backend:   "map",
			acl:       []string{"127.0.0.1"},
			badger:    nil,
			ristretto: nil,
		},
		{
			caddyfile: `mwcache badger`,
			valid:     true,
			backend:   "badger",
			acl:       []string{"127.0.0.1"},
			badger:    nil,
			ristretto: nil,
		},
		{
			caddyfile: `mwcache foo`,
			valid:     false,
			backend:   "",
			acl:       nil,
			badger:    nil,
			ristretto: nil,
		},
		{
			// 4
			caddyfile: `
			mwcache {
				purge_acl 11.11.11.11
			}
			`,
			valid:     true,
			backend:   "ristretto",
			acl:       []string{"11.11.11.11"},
			badger:    nil,
			ristretto: nil,
		},
		{
			// 5
			caddyfile: `
			mwcache {
				map
				purge_acl 11.11.11.11
			}
			`,
			valid:     true,
			backend:   "map",
			acl:       []string{"11.11.11.11"},
			badger:    nil,
			ristretto: nil,
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
			valid:     true,
			backend:   "ristretto",
			acl:       []string{"11.11.11.11", "11.11.11.12"},
			badger:    nil,
			ristretto: nil,
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
			valid:     true,
			backend:   "ristretto",
			acl:       []string{"11.11.11.11", "11.11.11.12", "11.11.11.13", "11.11.11.14"},
			badger:    nil,
			ristretto: nil,
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
		{
			caddyfile: `
			mwcache {
				badger {
					in_memory true
				}
			}
			`,
			valid:     true,
			backend:   "badger",
			acl:       []string{"127.0.0.1"},
			badger:    map[string]string{"in_memory": "true"},
			ristretto: nil,
		},
		{
			// 9
			caddyfile: `
			mwcache {
				badger {
					in_memory true
					value_log_file_size 8388608 # 1<23
				}
			}
			`,
			valid:     true,
			backend:   "badger",
			acl:       []string{"127.0.0.1"},
			badger:    map[string]string{"in_memory": "true", "value_log_file_size": "8388608"},
			ristretto: nil,
		},
		{
			//
			caddyfile: `
			mwcache {
				ristretto {
					num_counters 100000
					max_cost 10000
					buffer_items 64
				}
			}
			`,
			valid:     true,
			backend:   "ristretto",
			acl:       []string{"127.0.0.1"},
			badger:    nil,
			ristretto: map[string]string{"num_counters": "100000", "max_cost": "10000", "buffer_items": "64"},
		},
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

		for k, a := range m.config.BadgerConfig {
			e := test.badger[k]
			if a != e {
				t.Errorf("Test %d: Expected: '%s' but got '%s'", i, e, a)
			}
		}

		for k, a := range m.config.RistrettoConfig {
			e := test.ristretto[k]
			if a != e {
				t.Errorf("Test %d: Expected: '%s' but got '%s'", i, e, a)
			}
		}
	}
}
