package mwcache

import (
	"testing"
)

// The backend outlives a config reload, and is rebuilt only when its options
// change.
func TestSharedBackend(t *testing.T) {
	options := map[string]string{"num_counters": "1000", "max_cost": "100", "buffer_items": "64"}
	first, err := sharedBackend(Config{Backend: "ristretto", RistrettoConfig: options})
	if err != nil {
		t.Fatal(err)
	}
	again, err := sharedBackend(Config{Backend: "ristretto", RistrettoConfig: options})
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Error("the same options built a new backend")
	}
	changed, err := sharedBackend(Config{Backend: "ristretto", RistrettoConfig: map[string]string{
		"num_counters": "1000", "max_cost": "200", "buffer_items": "64",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Error("changed options kept the old backend")
	}

	for _, name := range []string{"", "redis"} {
		if _, err := sharedBackend(Config{Backend: name}); err == nil {
			t.Errorf("backend %q was accepted", name)
		}
	}
}
