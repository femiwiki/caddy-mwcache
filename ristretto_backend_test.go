package mwcache

import (
	"strings"
	"testing"
)

func newTestBackend(t *testing.T, key, budget string) *RistrettoBackend {
	t.Helper()
	b, err := newRistrettoBackend(map[string]string{
		"num_counters": "10000",
		key:            budget,
		"buffer_items": "64",
	})
	if err != nil {
		t.Fatalf("newRistrettoBackend: %v", err)
	}
	return b
}

// Under max_cost_bytes an entry costs the space it takes. ristretto adds its
// own per entry overhead on top, which is memory the cache really uses.
func TestMaxCostBytesSpendsTheSizeOfTheEntry(t *testing.T) {
	b := newTestBackend(t, "max_cost_bytes", "4194304")
	val := strings.Repeat("x", 4096)
	if err := b.put("/w/a", val); err != nil {
		t.Fatalf("put: %v", err)
	}
	b.wait()
	cost := b.cache.Metrics.CostAdded()
	if cost < uint64(len(val)) {
		t.Errorf("cost added is %d, which is below the %d bytes stored", cost, len(val))
	}
	if overhead := cost - uint64(len(val)); overhead > 64 {
		t.Errorf("cost added is %d for %d bytes stored, an overhead of %d", cost, len(val), overhead)
	}
}

// A budget in bytes has to hold the entries, so six of 256 KiB do not all fit
// in 1 MiB.
func TestABudgetInBytesIsSpentInBytes(t *testing.T) {
	b := newTestBackend(t, "max_cost_bytes", "1048576")
	val := strings.Repeat("x", 256*1024)
	for _, key := range []string{"/w/a", "/w/b", "/w/c", "/w/d", "/w/e", "/w/f"} {
		if err := b.put(key, val); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
		b.wait()
	}
	held := b.cache.Metrics.KeysAdded() - b.cache.Metrics.KeysEvicted()
	if held > 4 {
		t.Errorf("%d entries held on a budget of four", held)
	}
	if b.cache.Metrics.KeysEvicted() == 0 && b.cache.Metrics.SetsRejected() == 0 {
		t.Error("six entries of 256 KiB went into 1 MiB with nothing evicted or rejected")
	}
}

// max_cost keeps the meaning it had, so a config written against an earlier
// release keeps the cache it had.
func TestMaxCostStillCountsEntries(t *testing.T) {
	b := newTestBackend(t, "max_cost", "10000")
	val := strings.Repeat("x", 4096)
	if err := b.put("/w/a", val); err != nil {
		t.Fatalf("put: %v", err)
	}
	b.wait()
	if cost := b.cache.Metrics.CostAdded(); cost > 64 {
		t.Errorf("cost added is %d, so the entry was charged for its size", cost)
	}
	if added := b.cache.Metrics.KeysAdded(); added != 1 {
		t.Errorf("%d entries added, want 1", added)
	}
}

// The two names are one budget, so asking for both is a mistake worth
// reporting rather than resolving.
func TestBothBudgetNamesAtOnceIsRefused(t *testing.T) {
	options := map[string]string{
		"num_counters":   "10000",
		"max_cost":       "10000",
		"max_cost_bytes": "4194304",
		"buffer_items":   "64",
	}
	if _, err := newRistrettoBackend(options); err == nil {
		t.Error("both names at once were accepted")
	}
	if err := ValidateRistrettoConfig(options); err == nil {
		t.Error("Validate accepted both names at once")
	}
}

// The new name has to pass validation, which walks ristretto's own fields.
func TestMaxCostBytesPassesValidation(t *testing.T) {
	if err := ValidateRistrettoConfig(map[string]string{
		"num_counters":   "10000",
		"max_cost_bytes": "4194304",
		"buffer_items":   "64",
	}); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
