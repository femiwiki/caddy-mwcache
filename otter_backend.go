package mwcache

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/maypok86/otter/v2"
	"github.com/maypok86/otter/v2/stats"
)

// maxWeightBytesKey is the one option of the otter block: how many bytes the
// entries may take, counting each key and stored response.
const maxWeightBytesKey = "max_weight_bytes"

// OtterBackend is a Backend kept in memory by otter. Unlike ristretto, otter
// applies a write before it returns and never drops one, so a purge followed
// by a refill leaves the refilled entry in place.
type OtterBackend struct {
	cache   *otter.Cache[string, otterEntry]
	counter *stats.Counter
	// otter counts neither admissions nor updates, so new keys are counted
	// here, as ristretto's keys_added counts them.
	keysAdded atomic.Uint64
	closed    atomic.Bool
}

// otterEntry carries its own lifetime, so that the expiry calculator gives
// each entry the s-maxage it was stored with in the same write.
type otterEntry struct {
	val string
	ttl time.Duration
}

// noExpiry stands for an entry put without a ttl. otter adds the duration to
// the current time in nanoseconds, so it has to stay well short of the
// largest int64.
const noExpiry = 100 * 365 * 24 * time.Hour

type entryExpiry struct{}

func (entryExpiry) lifetime(e otterEntry) time.Duration {
	if e.ttl <= 0 {
		return noExpiry
	}
	return e.ttl
}

func (x entryExpiry) ExpireAfterCreate(e otter.Entry[string, otterEntry]) time.Duration {
	return x.lifetime(e.Value)
}

func (x entryExpiry) ExpireAfterUpdate(e otter.Entry[string, otterEntry], _ otterEntry) time.Duration {
	return x.lifetime(e.Value)
}

// ExpireAfterRead keeps the lifetime the last write gave: a read does not make
// a page fresh again.
func (entryExpiry) ExpireAfterRead(e otter.Entry[string, otterEntry]) time.Duration {
	return e.ExpiresAfter()
}

// weigh counts what the entry keeps in memory of its own, the key and the
// stored response. otter's per-entry overhead comes on top, as it does for
// ristretto.
func weigh(key string, e otterEntry) uint32 {
	return uint32(min(len(key)+len(e.val), math.MaxUint32))
}

// ValidateOtterConfig reports an error for options the otter backend would
// reject.
func ValidateOtterConfig(rawOptions map[string]string) error {
	_, err := parseOtterOptions(rawOptions)
	return err
}

func parseOtterOptions(rawOptions map[string]string) (uint64, error) {
	for k := range rawOptions {
		if k != maxWeightBytesKey {
			return 0, fmt.Errorf("unknown otter config: %s", k)
		}
	}
	v, ok := rawOptions[maxWeightBytesKey]
	if !ok {
		// The plugin cannot guess a size, as with ristretto, see #127
		return 0, fmt.Errorf("the otter block must set %s", maxWeightBytesKey)
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("%s must be a number of bytes above zero: %q", maxWeightBytesKey, v)
	}
	return n, nil
}

func newOtterBackend(rawOptions map[string]string) (*OtterBackend, error) {
	maxWeight, err := parseOtterOptions(rawOptions)
	if err != nil {
		return nil, err
	}
	counter := stats.NewCounter()
	cache, err := otter.New(&otter.Options[string, otterEntry]{
		MaximumWeight:    maxWeight,
		Weigher:          weigh,
		ExpiryCalculator: entryExpiry{},
		StatsRecorder:    counter,
	})
	if err != nil {
		return nil, err
	}
	return &OtterBackend{cache: cache, counter: counter}, nil
}

func (m *OtterBackend) get(key string) (string, error) {
	if m.closed.Load() {
		return "", ErrKeyNotFound
	}
	e, ok := m.cache.GetIfPresent(key)
	if !ok {
		return "", ErrKeyNotFound
	}
	return e.val, nil
}

func (m *OtterBackend) put(key string, val string, ttl time.Duration) error {
	if m.closed.Load() {
		return errClosed
	}
	if _, added := m.cache.Set(key, otterEntry{val: val, ttl: ttl}); added {
		m.keysAdded.Add(1)
	}
	return nil
}

func (m *OtterBackend) delete(key string) error {
	if m.closed.Load() {
		return nil
	}
	m.cache.Invalidate(key)
	return nil
}

func (m *OtterBackend) stats() (backendStats, bool) {
	s := m.counter.Snapshot()
	return backendStats{
		hits:        s.Hits,
		misses:      s.Misses,
		keysAdded:   m.keysAdded.Load(),
		keysEvicted: s.Evictions,
		// otter's admission policy does not refuse a write: a newcomer that
		// loses to the entry it would replace is evicted, and counted there.
		// Nor does it drop one.
		costUsed:    m.cache.WeightedSize(),
		costEvicted: s.EvictionWeight,
	}, true
}

// close drops the entries and stops otter's clean-up goroutine. A put that
// was already past its check may still land in the closed cache, which
// nothing reads any more.
func (m *OtterBackend) close() {
	if m.closed.Swap(true) {
		return
	}
	m.cache.InvalidateAll()
	m.cache.StopAllGoroutines()
}

var errTwoBackends = errors.New("the otter and ristretto blocks are two backends; say one")

// Interface guards
var (
	_ Backend       = (*OtterBackend)(nil)
	_ statsReporter = (*OtterBackend)(nil)
)
