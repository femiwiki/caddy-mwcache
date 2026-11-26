package mwcache

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// Caddy serves its own registry on the admin endpoint, so naming these
// caddy_mwcache_* puts them beside caddy_http_* in the same scrape.
const metricsNamespace, metricsSubsystem = "caddy", "mwcache"

// The counters the running backend keeps, read at scrape time rather than
// mirrored, under the same names whichever backend is running.
type statsCollector struct {
	read  func() (backendStats, bool)
	descs []*prometheus.Desc
	kinds []prometheus.ValueType
	value []func(backendStats) float64
}

func newStatsCollector(read func() (backendStats, bool)) *statsCollector {
	c := &statsCollector{read: read}
	addAs := func(kind prometheus.ValueType, name, help string, value func(backendStats) float64) {
		c.descs = append(c.descs, prometheus.NewDesc(
			prometheus.BuildFQName(metricsNamespace, metricsSubsystem, name), help, []string{"backend"}, nil,
		))
		c.kinds = append(c.kinds, kind)
		c.value = append(c.value, value)
	}
	add := func(name, help string, value func(backendStats) float64) {
		addAs(prometheus.CounterValue, name, help, value)
	}
	add("hits_total", "Responses served from the cache.",
		func(s backendStats) float64 { return float64(s.hits) })
	add("misses_total", "Requests the cache did not hold.",
		func(s backendStats) float64 { return float64(s.misses) })
	add("keys_added_total", "Entries admitted to the cache.",
		func(s backendStats) float64 { return float64(s.keysAdded) })
	add("keys_evicted_total", "Entries evicted to make room.",
		func(s backendStats) float64 { return float64(s.keysEvicted) })
	// A rejected set is the admission policy refusing an entry that is asked
	// for less often than the one it would evict, which is how a scan over
	// keys nobody asks for twice leaves the popular ones alone.
	add("sets_rejected_total", "Entries the admission policy refused.",
		func(s backendStats) float64 { return float64(s.setsRejected) })
	add("sets_dropped_total", "Entries dropped because the set buffer was full.",
		func(s backendStats) float64 { return float64(s.setsDropped) })
	addAs(prometheus.GaugeValue, "cost_used", "Cost of the entries the cache holds now.",
		func(s backendStats) float64 { return float64(s.costUsed) })
	add("cost_evicted_total", "Cost of the entries evicted.",
		func(s backendStats) float64 { return float64(s.costEvicted) })
	return c
}

// Describe implements prometheus.Collector.
func (c *statsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.descs {
		ch <- d
	}
}

// Collect implements prometheus.Collector. It reports nothing while no
// backend is running, or while the one running keeps no counts.
func (c *statsCollector) Collect(ch chan<- prometheus.Metric) {
	s, ok := c.read()
	if !ok {
		return
	}
	for i, d := range c.descs {
		ch <- prometheus.MustNewConstMetric(d, c.kinds[i], c.value[i](s), s.backend)
	}
}

// currentStats reads the counts of the backend that is running now, so a
// backend replaced by a config reload is not reported from a stale pointer.
func currentStats() (backendStats, bool) {
	backendMu.Lock()
	defer backendMu.Unlock()
	r, ok := backend.(statsReporter)
	if !ok {
		return backendStats{}, false
	}
	return r.stats()
}

// registerMetrics is called from Provision, which runs once per handler per
// config load, so the same collector is offered to the registry many times.
func registerMetrics(reg *prometheus.Registry, logger *zap.Logger) {
	if reg == nil {
		return
	}
	err := reg.Register(newStatsCollector(currentStats))
	var already prometheus.AlreadyRegisteredError
	if err != nil && !errors.As(err, &already) {
		logger.Warn("cache metrics are not registered", zap.Error(err))
	}
}
