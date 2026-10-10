package mwcache

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strconv"

	"github.com/dgraph-io/ristretto"
	"github.com/stoewer/go-strcase"
)

type RistrettoBackend struct {
	cache *ristretto.Cache
	// Set when the Caddyfile asked for max_cost_bytes, which is what makes an
	// entry cost the space it takes rather than one of however many the cache
	// holds.
	costInBytes bool
}

func newRistrettoBackend(rawOptions map[string]string) (*RistrettoBackend, error) {
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

// TODO
func ValidateRistrettoConfig(rawOptions map[string]string) error {
	options, _, err := readCostOption(rawOptions)
	if err != nil {
		return err
	}
	optionReflect := reflect.ValueOf(ristretto.Config{})
	for k := range options {
		k = strcase.UpperCamelCase(k)
		if !optionReflect.FieldByName(k).IsValid() {
			return fmt.Errorf("unknown config: %s", k)
		}
	}
	return nil
}

// TODO
func parseRistrettoOptions(rawOptions map[string]string) (*ristretto.Config, error) {
	// On by default so the counters exist to be scraped; an explicit
	// `metrics false` in the Caddyfile still wins, since rawOptions is
	// applied over this.
	c := ristretto.Config{Metrics: true}
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

func (m *RistrettoBackend) put(key string, val string) error {
	cost := int64(1)
	if m.costInBytes {
		cost = int64(len(val))
	}
	if ok := m.cache.Set(key, val, cost); !ok {
		return errors.New("set was dropped")
	}
	return nil
}

// wait blocks until the entries passed to put have been applied, which
// ristretto does asynchronously. Only tests need it.
func (m *RistrettoBackend) wait() {
	m.cache.Wait()
}

func (m *RistrettoBackend) get(key string) (string, error) {
	val, ok := m.cache.Get(key)
	if !ok {
		return "", ErrKeyNotFound
	}
	return val.(string), nil
}

func (m *RistrettoBackend) delete(key string) error {
	m.cache.Del(key)
	return nil
}

// Interface guards
var (
	_ Backend = (*RistrettoBackend)(nil)
)
