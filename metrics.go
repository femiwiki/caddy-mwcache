package mwcache

import (
	"errors"

	"github.com/dgraph-io/ristretto"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// Caddy serves its own registry on the admin endpoint, so naming these
// caddy_mwcache_* puts them beside caddy_http_* in the same scrape.
const metricsNamespace, metricsSubsystem = "caddy", "mwcache"

// The counters ristretto keeps. Each is read at scrape time rather than
// mirrored, so nothing has to be kept in step with the cache.
type ristrettoCollector struct {
	read  func() *ristretto.Metrics
	descs []*prometheus.Desc
	value []func(*ristretto.Metrics) float64
}

func newRistrettoCollector(read func() *ristretto.Metrics) *ristrettoCollector {
	c := &ristrettoCollector{read: read}
	add := func(name, help string, value func(*ristretto.Metrics) float64) {
		c.descs = append(c.descs, prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, name), help, nil, nil,
		))
		c.value = append(c.value, value)
	}
	add("hits_total", "Responses served from the cache.",
		func(m *ristretto.Metrics) float64 { return float64(m.Hits()) })
	add("misses_total", "Requests the cache did not hold.",
		func(m *ristretto.Metrics) float64 { return float64(m.Misses()) })
	add("keys_added_total", "Entries admitted to the cache.",
		func(m *ristretto.Metrics) float64 { return float64(m.KeysAdded()) })
	add("keys_evicted_total", "Entries evicted to make room.",
		func(m *ristretto.Metrics) float64 { return float64(m.KeysEvicted()) })
	// A rejected set is the admission policy refusing an entry that is asked
	// for less often than the one it would evict, which is how a scan over
	// keys nobody asks for twice leaves the popular ones alone.
	add("sets_rejected_total", "Entries the admission policy refused.",
		func(m *ristretto.Metrics) float64 { return float64(m.SetsRejected()) })
	add("sets_dropped_total", "Entries dropped because the set buffer was full.",
		func(m *ristretto.Metrics) float64 { return float64(m.SetsDropped()) })
	add("cost_added_total", "Cost of the entries admitted.",
		func(m *ristretto.Metrics) float64 { return float64(m.CostAdded()) })
	add("cost_evicted_total", "Cost of the entries evicted.",
		func(m *ristretto.Metrics) float64 { return float64(m.CostEvicted()) })
	return c
}

// Describe implements prometheus.Collector.
func (c *ristrettoCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.descs {
		ch <- d
	}
}

// Collect implements prometheus.Collector. It reports nothing while no
// ristretto backend is running, or while one runs with its counters off.
func (c *ristrettoCollector) Collect(ch chan<- prometheus.Metric) {
	m := c.read()
	if m == nil {
		return
	}
	for i, d := range c.descs {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, c.value[i](m))
	}
}

// currentMetrics reads the counters of the backend that is running now, so a
// backend replaced by a config reload is not reported from a stale pointer.
func currentMetrics() *ristretto.Metrics {
	backendMu.Lock()
	defer backendMu.Unlock()
	b, ok := backend.(*RistrettoBackend)
	if !ok {
		return nil
	}
	return b.cache.Metrics
}

// registerMetrics is called from Provision, which runs once per handler per
// config load, so the same collector is offered to the registry many times.
func registerMetrics(reg *prometheus.Registry, logger *zap.Logger) {
	if reg == nil {
		return
	}
	err := reg.Register(newRistrettoCollector(currentMetrics))
	var already prometheus.AlreadyRegisteredError
	if err != nil && !errors.As(err, &already) {
		logger.Warn("cache metrics are not registered", zap.Error(err))
	}
}
