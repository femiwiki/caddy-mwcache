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
