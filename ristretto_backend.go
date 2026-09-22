package mwcache

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/dgraph-io/ristretto"
	"github.com/stoewer/go-strcase"
)

type RistrettoBackend struct {
	cache *ristretto.Cache
}

func newRistrettoBackend(rawOptions map[string]string) (*RistrettoBackend, error) {
	if err := ValidateRistrettoConfig(rawOptions); err != nil {
		return nil, err
	}
	opt, err := parseRistrettoOptions(rawOptions)
	if err != nil {
		return nil, err
	}
	cache, err := ristretto.NewCache(opt)
	if err != nil {
		return nil, err
	}

	return &RistrettoBackend{cache}, nil
}

// ristretto.NewCache rejects a zero NumCounters, MaxCost or BufferItems, and
// how big the cache should be is not something this plugin can guess, so the
// Caddyfile has to say. See #127.
var requiredRistrettoOptions = []string{"num_counters", "max_cost", "buffer_items"}

// TODO
func ValidateRistrettoConfig(rawOptions map[string]string) error {
	optionReflect := reflect.ValueOf(ristretto.Config{})
	for k := range rawOptions {
		k = strcase.UpperCamelCase(k)
		if !optionReflect.FieldByName(k).IsValid() {
			return fmt.Errorf("unknown config: %s", k)
		}
	}
	var missing []string
	for _, k := range requiredRistrettoOptions {
		if rawOptions[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("the ristretto block must set %s", strings.Join(missing, ", "))
	}
	return nil
}

// TODO
func parseRistrettoOptions(rawOptions map[string]string) (*ristretto.Config, error) {
	c := ristretto.Config{}
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
	if ok := m.cache.Set(key, val, 1); !ok {
		return errors.New("set was dropped")
	}
	return nil
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
