package mwcache

import (
	"errors"
	"sync"
	"testing"
)

// The backend outlives a config reload, and is rebuilt only when its options
// change.
func TestSharedBackend(t *testing.T) {
	options := map[string]string{"num_counters": "1000", "max_cost": "100", "buffer_items": "64"}
	first, _, err := sharedBackend(Config{Backend: "ristretto", RistrettoConfig: options})
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := sharedBackend(Config{Backend: "ristretto", RistrettoConfig: options})
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Error("the same options built a new backend")
	}
	changed, _, err := sharedBackend(Config{Backend: "ristretto", RistrettoConfig: map[string]string{
		"num_counters": "1000", "max_cost": "200", "buffer_items": "64",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Error("changed options kept the old backend")
	}

	for _, name := range []string{"", "redis"} {
		if _, _, err := sharedBackend(Config{Backend: name}); err == nil {
			t.Errorf("backend %q was accepted", name)
		}
	}
}

func ristrettoOptions(maxCost string) Config {
	return Config{Backend: "ristretto", RistrettoConfig: map[string]string{
		"num_counters": "1000", "max_cost": maxCost, "buffer_items": "64",
	}}
}

// provision stands in for Caddy provisioning a handler.
func provision(t *testing.T, c Config) *Handler {
	t.Helper()
	b, release, err := sharedBackend(c)
	if err != nil {
		t.Fatal(err)
	}
	return &Handler{backend: b, release: release}
}

// cleanup stands in for Caddy cleaning up a handler.
func cleanup(t *testing.T, h *Handler) {
	t.Helper()
	if err := h.Cleanup(); err != nil {
		t.Fatal(err)
	}
}

func ristrettoBackend(t *testing.T, b Backend) *RistrettoBackend {
	t.Helper()
	rb, ok := b.(*RistrettoBackend)
	if !ok {
		t.Fatalf("got a %T backend", b)
	}
	return rb
}

func isClosed(t *testing.T, b Backend) bool {
	t.Helper()
	rb := ristrettoBackend(t, b)
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.closed
}

// A reload that changes the options closes the old backend once the last
// handler of the old config is cleaned up, and not before.
func TestReloadClosesReplacedBackend(t *testing.T) {
	// Caddy provisions the new config before it cleans up the old one
	old := []*Handler{provision(t, ristrettoOptions("301")), provision(t, ristrettoOptions("301"))}
	reloaded := provision(t, ristrettoOptions("302"))
	if reloaded.backend == old[0].backend {
		t.Fatal("changed options kept the old backend")
	}

	cleanup(t, old[0])
	if isClosed(t, old[1].backend) {
		t.Fatal("the backend was closed while a handler still held it")
	}
	// A second cleanup of the same handler counts once
	cleanup(t, old[0])
	if isClosed(t, old[1].backend) {
		t.Fatal("cleaning up one handler twice closed the backend")
	}
	cleanup(t, old[1])
	if !isClosed(t, old[1].backend) {
		t.Error("the replaced backend was not closed after its last handler")
	}

	// A closed backend answers as an empty one rather than panic
	b := old[1].backend
	if err := b.put("k", "v", 0); !errors.Is(err, errClosed) {
		t.Errorf("put on a closed backend returned %v", err)
	}
	if _, err := b.get("k"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("get on a closed backend returned %v", err)
	}
	if err := b.delete("k"); err != nil {
		t.Errorf("delete on a closed backend returned %v", err)
	}
	ristrettoBackend(t, b).wait()

	// The current backend stays open without handlers, for the next load
	cleanup(t, reloaded)
	if isClosed(t, reloaded.backend) {
		t.Error("the current backend was closed")
	}
	next := provision(t, ristrettoOptions("302"))
	if next.backend != reloaded.backend {
		t.Error("a load with the same options did not take the backend over")
	}
	cleanup(t, next)
}

// A backend built by a config load that failed has no handlers, so the next
// load that replaces it closes it at once.
func TestReloadClosesBackendOfFailedLoad(t *testing.T) {
	running := provision(t, ristrettoOptions("311"))
	failed := provision(t, ristrettoOptions("312"))
	cleanup(t, failed)
	if isClosed(t, failed.backend) {
		t.Fatal("the current backend was closed")
	}
	next := provision(t, ristrettoOptions("313"))
	if !isClosed(t, failed.backend) {
		t.Error("the backend nobody held was not closed when it was replaced")
	}
	if isClosed(t, running.backend) {
		t.Error("the backend of the running config was closed")
	}
	cleanup(t, running)
	cleanup(t, next)
}

// Requests still in flight on the old config may reach the backend while it
// closes.
func TestCloseDuringRequests(t *testing.T) {
	b, err := newRistrettoBackend(ristrettoOptions("321").RistrettoConfig)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 1000 {
				b.put("k", "v", 0) //nolint:errcheck // a put may fail once the backend is closed
				b.get("k")         //nolint:errcheck // a closed backend finds nothing
				b.delete("k")      //nolint:errcheck // only the absence of a panic matters
				b.wait()
			}
		})
	}
	b.close()
	wg.Wait()
	b.close()
}
