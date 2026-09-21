package mwcache

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func TestDirectives(t *testing.T) {
	testcases := []struct {
		caddyfile string
		valid     bool
		backend   string
		acl       []string
		ristretto map[string]string
	}{
		{
			caddyfile: `mwcache`,
			valid:     true,
			backend:   "ristretto",
			acl:       []string{"127.0.0.1"},
			ristretto: nil,
		},
		{
			caddyfile: `mwcache foo`,
			valid:     false,
			backend:   "",
			acl:       nil,
			ristretto: nil,
		},
		{
			caddyfile: `
			mwcache {
				purge_acl 11.11.11.11
			}
			`,
			valid:     true,
			backend:   "ristretto",
			acl:       []string{"11.11.11.11"},
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
		if m.Config.Backend != test.backend {
			t.Errorf("Test %d: Expected: '%s' but got '%s'", i, test.backend, m.Config.Backend)
		}

		// TODO Compare all elements
		if len(m.Config.PurgeAcl) != len(test.acl) || m.Config.PurgeAcl[0] != test.acl[0] {
			e := strings.Join(test.acl, ", ")
			a := strings.Join(m.Config.PurgeAcl, ", ")
			t.Errorf("Test %d: Expected: '%s' but got '%s'", i, e, a)
		}

		for k, a := range m.Config.RistrettoConfig {
			e := test.ristretto[k]
			if a != e {
				t.Errorf("Test %d: Expected: '%s' but got '%s'", i, e, a)
			}
		}
	}
}

// A config that is loaded as JSON never runs the Caddyfile adapter, so the
// handler has to carry its own options instead of reading them from a
// package-level variable. See #119.
func TestJSONRoundTrip(t *testing.T) {
	d := caddyfile.NewTestDispenser(`
	mwcache {
		ristretto {
			num_counters 100000
			max_cost 10000
			buffer_items 64
		}
		purge_acl 11.11.11.11
	}
	`)
	adapted := &Handler{}
	if err := adapted.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	adaptedJSON, err := json.Marshal(adapted)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// The adapted JSON is all that a process loading a JSON config ever sees.
	loaded := &Handler{}
	if err := json.Unmarshal(adaptedJSON, loaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := loaded.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if loaded.backend == nil {
		t.Error("Expected a backend but got none")
	}
	if loaded.Config.Backend != "ristretto" {
		t.Errorf("Expected: 'ristretto' but got '%s'", loaded.Config.Backend)
	}
	if acl := strings.Join(loaded.Config.PurgeAcl, ", "); acl != "11.11.11.11" {
		t.Errorf("Expected: '11.11.11.11' but got '%s'", acl)
	}
	if cost := loaded.Config.RistrettoConfig["max_cost"]; cost != "10000" {
		t.Errorf("Expected: '10000' but got '%s'", cost)
	}
}

// Provisioning a handler that carries no options must report an error instead
// of dereferencing a nil config. See #119.
func TestProvisionWithoutOptions(t *testing.T) {
	h := &Handler{}
	if err := json.Unmarshal([]byte(`{}`), h); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := h.Provision(caddy.Context{}); err == nil {
		t.Error("Error should be thrown")
	}
}

// The backend is shared so that a reload keeps the cache, and is rebuilt only
// when the backend options change.
func TestBackendIsCreatedOnce(t *testing.T) {
	options := map[string]string{"num_counters": "200000", "max_cost": "20000", "buffer_items": "64"}
	newHandler := func(o map[string]string) *Handler {
		return &Handler{Config: Config{
			Backend:         "ristretto",
			PurgeAcl:        []string{"127.0.0.1"},
			RistrettoConfig: o,
		}}
	}

	first := newHandler(options)
	reloaded := newHandler(map[string]string{"num_counters": "200000", "max_cost": "20000", "buffer_items": "64"})
	reconfigured := newHandler(map[string]string{"num_counters": "300000", "max_cost": "30000", "buffer_items": "64"})
	for i, h := range []*Handler{first, reloaded, reconfigured} {
		if err := h.Provision(caddy.Context{}); err != nil {
			t.Fatalf("Test %d: Provision: %v", i, err)
		}
	}

	if first.backend != reloaded.backend {
		t.Error("The backend should be reused so that a reload keeps the cache")
	}
	if first.backend == reconfigured.backend {
		t.Error("The backend should be rebuilt when its options change")
	}
}
