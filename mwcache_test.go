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
	}{
		{
			caddyfile: `mwcache`,
			valid:     true,
			backend:   "badger",
			acl:       []string{"127.0.0.1"},
			badger:    nil,
		},
		{
			caddyfile: `mwcache map`,
			valid:     true,
			backend:   "map",
			acl:       []string{"127.0.0.1"},
			badger:    nil,
		},
		{
			caddyfile: `mwcache badger`,
			valid:     true,
			backend:   "badger",
			acl:       []string{"127.0.0.1"},
			badger:    nil,
		},
		{
			caddyfile: `mwcache foo`,
			valid:     false,
			backend:   "",
			acl:       nil,
			badger:    nil,
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
			badger:  nil,
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
			badger:  nil,
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
			badger:  nil,
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
			badger:  nil,
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
			valid:   true,
			backend: "badger",
			acl:     []string{"127.0.0.1"},
			badger:  map[string]string{"in_memory": "true"},
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
			valid:   true,
			backend: "badger",
			acl:     []string{"127.0.0.1"},
			badger:  map[string]string{"in_memory": "true", "value_log_file_size": "8388608"},
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
	}
}

func TestCIDRContainsIP(t *testing.T) {
	for i, test := range []struct {
		cidr     string
		ip       string
		expected bool
	}{
		{"10.0.0.0/8", "10.0.0.4", true},
		{"10.0.0.0/8", "111.0.0.4", false},
		{"10.0.0.0/8", "111.0.0.4:34567", false},
		{"127.0.0.1", "127.0.0.1", true},
		{"127.0.0.1", "127.0.0.1:4567", true},
	} {
		actual := CIDRContainsIP(test.cidr, test.ip)
		if test.expected != actual {
			t.Errorf("Test %d: Expected: '%t' but got '%t'", i, test.expected, actual)
		}
	}
}
