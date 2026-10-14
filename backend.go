// Package mwcache is a Caddy HTTP handler that caches MediaWiki page views.
package mwcache

import (
	"errors"
)

var (
	// ErrKeyNotFound is returned by a backend that does not hold the key.
	ErrKeyNotFound = errors.New("key not found")
)

// Backend stores cached responses by key.
type Backend interface {
	get(key string) (string, error)
	put(key string, val string) error
	delete(key string) error
}
