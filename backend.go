package mwcache

import (
	"errors"
)

var (
	ErrKeyNotFound = errors.New("Key not found")
)

type Backend interface {
	get(key string) (string, error)
	put(key string, val string) error
}
