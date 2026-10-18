package mwcache

import (
	"sync"
	"time"
)

// passTTL is how long a key whose response could not be shared skips the
// wait, the two minutes Varnish's builtin VCL gives a hit-for-pass object.
const passTTL = 2 * time.Minute

// A flight is one upstream fetch of a key. Its waiters are handed entry, as
// stored in the cache, or nil when it cannot be shared.
type flight struct {
	done    chan struct{}
	entry   []byte
	waiting int
}

// coalescer lets concurrent misses for one key share one upstream request,
// like https://wikitech.wikimedia.org/wiki/Varnish#Requests_coalescing
type coalescer struct {
	mu      sync.Mutex
	flights map[string]*flight
	pass    map[string]time.Time
	swept   time.Time
	now     func() time.Time
}

func newCoalescer() *coalescer {
	return &coalescer{
		flights: map[string]*flight{},
		pass:    map[string]time.Time{},
		now:     time.Now,
	}
}

// It is global, as the backend is, so that every server block serving a host
// shares the requests in flight for it.
var misses = newCoalescer()

// join returns the flight to wait on for key, or, when the caller should go
// upstream itself, nil and whether it leads a flight that it must then land.
func (c *coalescer) join(key string) (f *flight, leader bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if until, ok := c.pass[key]; ok {
		if c.now().Before(until) {
			return nil, false
		}
		delete(c.pass, key)
	}
	if f, ok := c.flights[key]; ok {
		f.waiting++
		return f, false
	}
	c.flights[key] = &flight{done: make(chan struct{})}
	return nil, true
}

// land hands entry to the waiters on key and returns how many there were. A
// nil entry marks the key as pass, unless it came from an upstream error.
func (c *coalescer) land(key string, entry []byte, failed bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.flights[key]
	delete(c.flights, key)
	now := c.now()
	if entry == nil && !failed {
		c.pass[key] = now.Add(passTTL)
	}
	// Keys marked once and never asked for again would otherwise pile up
	if now.Sub(c.swept) > passTTL {
		for k, until := range c.pass {
			if !now.Before(until) {
				delete(c.pass, k)
			}
		}
		c.swept = now
	}
	if f == nil {
		return 0
	}
	f.entry = entry
	close(f.done)
	return f.waiting
}

// waiters returns how many requests wait on the flight for key.
func (c *coalescer) waiters(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if f, ok := c.flights[key]; ok {
		return f.waiting
	}
	return 0
}

// unpass lets misses for key wait again, once its response can be shared.
func (c *coalescer) unpass(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pass, key)
}
