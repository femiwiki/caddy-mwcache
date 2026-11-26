// Package mwcache is a Caddy HTTP handler that caches MediaWiki page views.
package mwcache

import (
	"errors"
	"time"
)

var (
	// ErrKeyNotFound is returned by a backend that does not hold the key.
	ErrKeyNotFound = errors.New("key not found")
)

// Backend stores cached responses by key.
type Backend interface {
	get(key string) (string, error)
	// put stores val under key. A ttl above zero is how long the entry may
	// be kept; zero keeps it until it is evicted.
	put(key string, val string, ttl time.Duration) error
	delete(key string) error
	// close stops the backend and drops what it holds. The other methods
	// still return safely afterwards.
	close()
}

// backendStats is what a backend reports for the caddy_mwcache_* series. A
// backend that has no such count leaves it at zero.
type backendStats struct {
	// backend names the backend, as the Caddyfile does, for the backend label.
	backend      string
	hits         uint64
	misses       uint64
	keysAdded    uint64
	keysEvicted  uint64
	setsRejected uint64
	setsDropped  uint64
	// costUsed is the cost of the entries held now, which drops when an entry
	// is replaced by a cheaper one or deleted, so it is a gauge.
	costUsed    uint64
	costEvicted uint64
}

// statsReporter is a Backend that keeps counts for the metrics. stats reports
// false while it keeps none, such as a ristretto backend with metrics off.
type statsReporter interface {
	stats() (backendStats, bool)
}
