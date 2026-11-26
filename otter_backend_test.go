package mwcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/prometheus/client_golang/prometheus"
)

func newTestOtterBackend(t *testing.T, maxWeightBytes string) *OtterBackend {
	t.Helper()
	b, err := newOtterBackend(map[string]string{maxWeightBytesKey: maxWeightBytes})
	if err != nil {
		t.Fatalf("newOtterBackend: %v", err)
	}
	t.Cleanup(b.close)
	return b
}

// otter applies a write before it returns, so a read right after a put sees
// it, with no wait as ristretto needs.
func TestOtterPutIsVisibleAtOnce(t *testing.T) {
	b := newTestOtterBackend(t, "1048576")
	if _, err := b.get("/w/a"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("get before put: %v, want ErrKeyNotFound", err)
	}
	if err := b.put("/w/a", "page", 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got, err := b.get("/w/a"); err != nil || got != "page" {
		t.Fatalf("get: %q, %v", got, err)
	}
	if err := b.delete("/w/a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := b.get("/w/a"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("get after delete: %v, want ErrKeyNotFound", err)
	}
}

// A purge is followed by a refill of the same key. ristretto can lose a Set
// right after a Del of its key (ristretto#506); otter must not.
func TestOtterRefillAfterPurgeIsKept(t *testing.T) {
	b := newTestOtterBackend(t, "1048576")
	for i := range 1000 {
		want := fmt.Sprintf("revision %d", i)
		if err := b.delete("/w/a"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if err := b.put("/w/a", want, time.Hour); err != nil {
			t.Fatalf("put: %v", err)
		}
		if got, err := b.get("/w/a"); err != nil || got != want {
			t.Fatalf("round %d: get gave %q, %v, want %q", i, got, err, want)
		}
	}
}

// An entry weighs its key and stored response, so the budget is in bytes.
func TestOtterWeighsKeyAndValue(t *testing.T) {
	b := newTestOtterBackend(t, "1048576")
	key, val := "/w/a", strings.Repeat("x", 4096)
	if err := b.put(key, val, 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	// The weight is applied by otter's maintenance, which may still be pending
	b.cache.CleanUp()
	if got, want := b.cache.WeightedSize(), uint64(len(key)+len(val)); got != want {
		t.Errorf("weighted size is %d, want %d", got, want)
	}
	if got := mustStats(t, b).costUsed; got != uint64(len(key)+len(val)) {
		t.Errorf("cost used is %d, want %d", got, len(key)+len(val))
	}
}

// Filling past the budget evicts until what is held fits it again.
func TestOtterStaysWithinItsBudget(t *testing.T) {
	const budget = 64 * 1024
	b := newTestOtterBackend(t, strconv.Itoa(budget))
	val := strings.Repeat("x", 4096)
	for i := range 100 {
		if err := b.put(fmt.Sprintf("/w/%d", i), val, 0); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	b.cache.CleanUp()
	s := mustStats(t, b)
	if s.costUsed > budget {
		t.Errorf("cost used is %d, above the budget of %d", s.costUsed, budget)
	}
	if s.keysEvicted == 0 || s.costEvicted == 0 {
		t.Errorf("nothing was evicted: %+v", s)
	}
	if s.keysAdded != 100 {
		t.Errorf("keys added is %d, want 100", s.keysAdded)
	}
}

// An entry put with a ttl is gone once it runs out, though it was read in
// between, and one put without stays.
func TestOtterEntryExpiresAfterItsTTL(t *testing.T) {
	b := newTestOtterBackend(t, "1048576")
	if err := b.put("/w/short", "x", 100*time.Millisecond); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := b.put("/w/forever", "x", 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := b.get("/w/short"); err != nil {
		t.Fatalf("get before the ttl ran out: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := b.get("/w/short"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("get after the ttl ran out: %v, want ErrKeyNotFound", err)
	}
	if _, err := b.get("/w/forever"); err != nil {
		t.Errorf("get of the entry without a ttl: %v", err)
	}
}

// Each entry keeps the lifetime it was last written with: a rewrite replaces
// it.
func TestOtterLifetimeFollowsWritesOnly(t *testing.T) {
	b := newTestOtterBackend(t, "1048576")
	// otter reports the time left, which is a little short of the lifetime
	// by the time it is read, so it is rounded up to the second.
	expiresAfter := func() time.Duration {
		t.Helper()
		e, ok := b.cache.GetEntryQuietly("/w/a")
		if !ok {
			t.Fatal("the entry is missing")
		}
		return e.ExpiresAfter().Round(time.Second)
	}
	if err := b.put("/w/a", "x", time.Hour); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := expiresAfter(); got != time.Hour {
		t.Errorf("lifetime after put is %v, want 1h", got)
	}
	if err := b.put("/w/a", "y", time.Minute); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := expiresAfter(); got != time.Minute {
		t.Errorf("lifetime after a rewrite is %v, want 1m", got)
	}
	if err := b.put("/w/a", "z", 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := expiresAfter(); got != noExpiry {
		t.Errorf("lifetime after a put without ttl is %v, want %v", got, noExpiry)
	}
}

// Hits and misses come from otter's counter, and a key is counted as added
// once, not again when it is replaced.
func TestOtterStats(t *testing.T) {
	b := newTestOtterBackend(t, "1048576")
	_, _ = b.get("/w/a")
	_ = b.put("/w/a", "x", 0)
	_ = b.put("/w/a", "y", 0)
	_, _ = b.get("/w/a")
	_, _ = b.get("/w/a")
	s := mustStats(t, b)
	if s.hits != 2 || s.misses != 1 || s.keysAdded != 1 {
		t.Errorf("stats are %+v, want 2 hits, 1 miss, 1 key added", s)
	}
	if s.backend != "otter" {
		t.Errorf("backend is %q, want otter", s.backend)
	}
	if s.setsRejected != 0 || s.setsDropped != 0 {
		t.Errorf("otter reported rejected or dropped sets: %+v", s)
	}
}

// After close the methods still return safely, and the entries are gone.
func TestOtterAfterClose(t *testing.T) {
	b := newTestOtterBackend(t, "1048576")
	if err := b.put("/w/a", "x", 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	b.close()
	b.close()
	if _, err := b.get("/w/a"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("get after close: %v, want ErrKeyNotFound", err)
	}
	if err := b.put("/w/a", "x", 0); !errors.Is(err, errClosed) {
		t.Errorf("put after close: %v, want errClosed", err)
	}
	if err := b.delete("/w/a"); err != nil {
		t.Errorf("delete after close: %v", err)
	}
	if n := b.cache.EstimatedSize(); n != 0 {
		t.Errorf("%d entries are left after close", n)
	}
}

func TestOtterOptions(t *testing.T) {
	for _, tc := range []struct {
		options map[string]string
		valid   bool
	}{
		{map[string]string{"max_weight_bytes": "67108864"}, true},
		{nil, false},
		{map[string]string{}, false},
		{map[string]string{"max_weight_bytes": "0"}, false},
		{map[string]string{"max_weight_bytes": "-1"}, false},
		{map[string]string{"max_weight_bytes": "64MiB"}, false},
		{map[string]string{"max_weight_bytes": "1024", "num_counters": "1000"}, false},
	} {
		err := ValidateOtterConfig(tc.options)
		if tc.valid && err != nil {
			t.Errorf("%v: %v", tc.options, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("%v was accepted", tc.options)
		}
	}
}

func TestOtterDirectives(t *testing.T) {
	for _, tc := range []struct {
		caddyfile string
		valid     bool
	}{
		{`mwcache {
			otter {
				max_weight_bytes 67108864
			}
		}`, true},
		{`mwcache otter {
			otter {
				max_weight_bytes 67108864
			}
		}`, true},
		// The plugin cannot guess a size
		{`mwcache otter`, false},
		{`mwcache {
			otter
		}`, false},
		{`mwcache {
			otter {
				max_weight_bytes
			}
		}`, false},
		{`mwcache {
			otter {
				max_weight_bytes 1 2
			}
		}`, false},
		{`mwcache {
			otter {
				max_weight_bytes 67108864
			}
			ristretto {
				num_counters 1000
				max_cost 100
				buffer_items 64
			}
		}`, false},
		{`mwcache {
			ristretto {
				num_counters 1000
				max_cost 100
				buffer_items 64
			}
			otter {
				max_weight_bytes 67108864
			}
		}`, false},
	} {
		h := &Handler{}
		err := h.UnmarshalCaddyfile(caddyfile.NewTestDispenser(tc.caddyfile))
		if err == nil {
			err = h.Validate()
		}
		if tc.valid && err != nil {
			t.Errorf("%s: %v", tc.caddyfile, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("%s was accepted", tc.caddyfile)
		}
		if tc.valid && (h.Config.Backend != "otter" || h.Config.OtterConfig[maxWeightBytesKey] != "67108864") {
			t.Errorf("%s: config is %+v", tc.caddyfile, h.Config)
		}
	}
}

// The otter options survive the trip through the adapted JSON, and the
// handler that loads it reports the same caddy_mwcache_* series.
func TestOtterFromJSON(t *testing.T) {
	adapted := &Handler{}
	if err := adapted.UnmarshalCaddyfile(caddyfile.NewTestDispenser(`mwcache {
		otter {
			max_weight_bytes 1048576
		}
	}`)); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	adaptedJSON, err := json.Marshal(adapted)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	loaded := &Handler{}
	if err := json.Unmarshal(adaptedJSON, loaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := loaded.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	t.Cleanup(func() { _ = loaded.Cleanup() })
	if _, ok := loaded.backend.(*OtterBackend); !ok {
		t.Fatalf("the backend is a %T, want *OtterBackend", loaded.backend)
	}
	if _, err := loaded.backend.get("/w/a"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("get: %v", err)
	}

	reg := prometheus.NewRegistry()
	registerMetrics(reg, loaded.logger)
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(families) != 8 {
		t.Errorf("gathered %d families, want 8", len(families))
	}
	for _, f := range families {
		if f.GetName() == "caddy_mwcache_misses_total" && f.GetMetric()[0].GetCounter().GetValue() < 1 {
			t.Error("caddy_mwcache_misses_total did not count the miss")
		}
	}
}

// Switching between backends, or changing the otter budget, builds a new
// backend; the same options keep it.
func TestOtterSharedBackend(t *testing.T) {
	otterConfig := func(n string) Config {
		return Config{Backend: "otter", OtterConfig: map[string]string{maxWeightBytesKey: n}}
	}
	first, _, err := sharedBackend(otterConfig("1048576"))
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := sharedBackend(otterConfig("1048576"))
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Error("the same options built a new backend")
	}
	changed, _, err := sharedBackend(otterConfig("2097152"))
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Error("a changed budget kept the old backend")
	}
	switched, _, err := sharedBackend(ristrettoOptions("100"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := switched.(*RistrettoBackend); !ok {
		t.Errorf("switching to ristretto gave a %T", switched)
	}
}
