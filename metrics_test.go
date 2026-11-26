package mwcache

import (
	"errors"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// A miss is counted in Get itself, unlike a set, which ristretto applies
// asynchronously, so it is the one counter a test can read without waiting.
func TestMetricsAreCollected(t *testing.T) {
	h := &Handler{Config: Config{
		Backend:         "ristretto",
		RistrettoConfig: map[string]string{"num_counters": "1000", "max_cost": "100", "buffer_items": "64"},
	}}
	if err := h.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if _, err := h.backend.get("a key nothing put"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("get: %v", err)
	}

	reg := prometheus.NewRegistry()
	registerMetrics(reg, h.logger)
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	got := map[string]float64{}
	for _, f := range families {
		if f.GetType() == dto.MetricType_GAUGE {
			got[f.GetName()] = f.GetMetric()[0].GetGauge().GetValue()
		} else {
			got[f.GetName()] = f.GetMetric()[0].GetCounter().GetValue()
		}
	}

	for _, name := range []string{
		"caddy_mwcache_hits_total",
		"caddy_mwcache_misses_total",
		"caddy_mwcache_keys_added_total",
		"caddy_mwcache_keys_evicted_total",
		"caddy_mwcache_sets_rejected_total",
		"caddy_mwcache_sets_dropped_total",
		"caddy_mwcache_cost_used",
		"caddy_mwcache_cost_evicted_total",
	} {
		if _, ok := got[name]; !ok {
			t.Errorf("%s is missing", name)
		}
	}
	if got["caddy_mwcache_misses_total"] < 1 {
		t.Errorf("caddy_mwcache_misses_total is %v, want at least 1", got["caddy_mwcache_misses_total"])
	}
}

// Replacing an entry with a cheaper one lowers ristretto's added cost, which a
// counter must never do, so the cost is reported as what the cache holds.
func TestCostUsedFollowsACheaperReplacement(t *testing.T) {
	b := newTestBackend(t, "max_cost_bytes", "4194304")
	if err := b.put("/w/a", strings.Repeat("x", 8192), 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	b.wait()
	before := mustStats(t, b).costUsed
	if err := b.put("/w/a", strings.Repeat("x", 1024), 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	b.wait()
	after := mustStats(t, b).costUsed
	if after >= before {
		t.Fatalf("cost used went from %v to %v, want it lower", before, after)
	}

	c := newStatsCollector(b.stats)
	reg := prometheus.NewRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != "caddy_mwcache_cost_used" {
			continue
		}
		if f.GetType() != dto.MetricType_GAUGE {
			t.Errorf("caddy_mwcache_cost_used is a %v, want a gauge", f.GetType())
		}
		if v := f.GetMetric()[0].GetGauge().GetValue(); v != float64(after) {
			t.Errorf("caddy_mwcache_cost_used is %v, want %v", v, after)
		}
		return
	}
	t.Error("caddy_mwcache_cost_used is missing")
}

// Offering the collector again, as every config reload does, must not fail and
// must not leave the registry reporting the same counter twice.
func TestMetricsRegisterIsIdempotent(t *testing.T) {
	reg := prometheus.NewRegistry()
	registerMetrics(reg, nil)
	registerMetrics(reg, nil)
	if _, err := reg.Gather(); err != nil {
		t.Fatalf("Gather: %v", err)
	}
}

func mustStats(t *testing.T, r statsReporter) backendStats {
	t.Helper()
	s, ok := r.stats()
	if !ok {
		t.Fatal("the backend keeps no counts")
	}
	return s
}

// The series come from backendStats, so a backend other than ristretto
// reports the same names, each with its own value.
func TestMetricsReportAnyBackend(t *testing.T) {
	want := backendStats{
		hits: 1, misses: 2, keysAdded: 3, keysEvicted: 4,
		setsRejected: 5, setsDropped: 6, costUsed: 7, costEvicted: 8,
	}
	c := newStatsCollector(func() (backendStats, bool) { return want, true })
	reg := prometheus.NewRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	got := map[string]float64{}
	for _, f := range families {
		if f.GetType() == dto.MetricType_GAUGE {
			got[f.GetName()] = f.GetMetric()[0].GetGauge().GetValue()
		} else {
			got[f.GetName()] = f.GetMetric()[0].GetCounter().GetValue()
		}
	}
	for name, v := range map[string]float64{
		"caddy_mwcache_hits_total":          1,
		"caddy_mwcache_misses_total":        2,
		"caddy_mwcache_keys_added_total":    3,
		"caddy_mwcache_keys_evicted_total":  4,
		"caddy_mwcache_sets_rejected_total": 5,
		"caddy_mwcache_sets_dropped_total":  6,
		"caddy_mwcache_cost_used":           7,
		"caddy_mwcache_cost_evicted_total":  8,
	} {
		if got[name] != v {
			t.Errorf("%s is %v, want %v", name, got[name], v)
		}
	}
}

// A ristretto backend with `metrics false` keeps no counts, and a scrape
// reports nothing rather than zeros.
func TestRistrettoWithoutMetricsKeepsNoCounts(t *testing.T) {
	b, err := newRistrettoBackend(map[string]string{
		"num_counters": "1000", "max_cost": "100", "buffer_items": "64", "metrics": "false",
	})
	if err != nil {
		t.Fatalf("newRistrettoBackend: %v", err)
	}
	t.Cleanup(b.close)
	if _, ok := b.stats(); ok {
		t.Error("stats reported counts with metrics off")
	}
}

// Without a backend that keeps counts there is nothing to read, and a scrape
// still has to succeed.
func TestMetricsWithoutBackend(t *testing.T) {
	c := newStatsCollector(func() (backendStats, bool) { return backendStats{}, false })
	reg := prometheus.NewRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(families) != 0 {
		t.Errorf("gathered %d families, want none", len(families))
	}
}
