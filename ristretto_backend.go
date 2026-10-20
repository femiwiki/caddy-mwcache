package mwcache

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto/v2"
	"github.com/stoewer/go-strcase"
)

// RistrettoBackend is a Backend kept in memory by ristretto.
type RistrettoBackend struct {
	cache *ristretto.Cache[string, string]
	// Set when the Caddyfile asked for max_cost_bytes, which is what makes an
	// entry cost the space it takes rather than one of however many the cache
	// holds.
	costInBytes bool
	// mu keeps close from running while a request is inside the cache, as
	// ristretto's Set, Del and Wait send on a channel that Close closes.
	mu     sync.RWMutex
	closed bool
}

func newRistrettoBackend(rawOptions map[string]string) (*RistrettoBackend, error) {
	if err := ValidateRistrettoConfig(rawOptions); err != nil {
		return nil, err
	}
	options, costInBytes, err := readCostOption(rawOptions)
	if err != nil {
		return nil, err
	}
	opt, err := parseRistrettoOptions(options)
	if err != nil {
		return nil, err
	}
	cache, err := ristretto.NewCache(opt)
	if err != nil {
		return nil, err
	}

	return &RistrettoBackend{cache: cache, costInBytes: costInBytes}, nil
}

// max_cost_bytes is ristretto's MaxCost spent in bytes. It is a second name
// rather than a new meaning for max_cost, so a config written against an
// earlier release keeps the cache it had: under max_cost an entry goes on
// costing 1, and the number goes on meaning entries.
const (
	costKey      = "max_cost"
	costBytesKey = "max_cost_bytes"
)

func readCostOption(rawOptions map[string]string) (map[string]string, bool, error) {
	v, inBytes := rawOptions[costBytesKey]
	if !inBytes {
		return rawOptions, false, nil
	}
	if _, also := rawOptions[costKey]; also {
		return nil, false, fmt.Errorf("%s and %s are the same budget; say one", costKey, costBytesKey)
	}
	options := maps.Clone(rawOptions)
	delete(options, costBytesKey)
	options[costKey] = v
	return options, true, nil
}

// ristretto.NewCache rejects a zero value for each of these, and the plugin
// cannot guess a size, so the Caddyfile has to say. See #127.
var requiredRistrettoOptions = []string{"num_counters", "max_cost", "buffer_items"}

// ValidateRistrettoConfig reports an error for options ristretto would reject.
func ValidateRistrettoConfig(rawOptions map[string]string) error {
	options, _, err := readCostOption(rawOptions)
	if err != nil {
		return err
	}
	optionReflect := reflect.ValueOf(ristretto.Config[string, string]{})
	for k := range options {
		k = strcase.UpperCamelCase(k)
		if !optionReflect.FieldByName(k).IsValid() {
			return fmt.Errorf("unknown config: %s", k)
		}
	}
	var missing []string
	for _, k := range requiredRistrettoOptions {
		if options[k] != "" {
			continue
		}
		if k == costKey {
			k = costBytesKey + " or " + costKey
		}
		missing = append(missing, k)
	}
	if len(missing) != 0 {
		return fmt.Errorf("the ristretto block must set %s", strings.Join(missing, ", "))
	}
	return nil
}

// TODO
func parseRistrettoOptions(rawOptions map[string]string) (*ristretto.Config[string, string], error) {
	// On by default so the counters exist to be scraped; an explicit
	// `metrics false` in the Caddyfile still wins, since rawOptions is
	// applied over this.
	c := ristretto.Config[string, string]{Metrics: true}
	optionsReflect := reflect.ValueOf(&c)
	for k, strV := range rawOptions {
		k = strcase.UpperCamelCase(k)
		field := optionsReflect.Elem().FieldByName(k)
		switch field.Type().String() {
		case "string":
			field.SetString(strV)
		case "bool":
			v, err := strconv.ParseBool(strV)
			if err != nil {
				return nil, err
			}
			field.SetBool(v)
		case "int":
			v, err := strconv.ParseInt(strV, 10, 64)
			if err != nil {
				return nil, err
			}
			field.SetInt(v)
		case "int32":
			v, err := strconv.ParseInt(strV, 10, 64)
			if err != nil {
				return nil, err
			}
			field.SetInt(v)
		case "int64":
			v, err := strconv.ParseInt(strV, 10, 64)
			if err != nil {
				return nil, err
			}
			field.SetInt(v)
		case "float64":
			v, err := strconv.ParseFloat(strV, 64)
			if err != nil {
				return nil, err
			}
			field.SetFloat(v)
		}
	}

	return &c, nil
}

var errClosed = errors.New("cache is closed")

func (m *RistrettoBackend) put(key string, val string, ttl time.Duration) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return errClosed
	}
	cost := int64(1)
	if m.costInBytes {
		cost = int64(len(val))
	}
	if ok := m.cache.SetWithTTL(key, val, cost, ttl); !ok {
		return errors.New("set was dropped")
	}
	return nil
}

// wait blocks until the entries passed to put have been applied, which
// ristretto does asynchronously. Only tests need it.
func (m *RistrettoBackend) wait() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return
	}
	m.cache.Wait()
}

func (m *RistrettoBackend) get(key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return "", ErrKeyNotFound
	}
	val, ok := m.cache.Get(key)
	if !ok {
		return "", ErrKeyNotFound
	}
	return val, nil
}

func (m *RistrettoBackend) delete(key string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil
	}
	m.cache.Del(key)
	return nil
}

func (m *RistrettoBackend) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	m.cache.Close()
}

// Interface guards
var (
	_ Backend = (*RistrettoBackend)(nil)
)
