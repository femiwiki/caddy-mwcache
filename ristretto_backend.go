package mwcache

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/dgraph-io/ristretto"
	"github.com/stoewer/go-strcase"
)

type RistrettoBackend struct {
	cache *ristretto.Cache
}

func newRistrettoBackend(rawOptions map[string]string) (*RistrettoBackend, error) {
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

// TODO
func ValidateRistrettoConfig(rawOptions map[string]string) error {
	optionReflect := reflect.ValueOf(ristretto.Config{})
	for k := range rawOptions {
		k = strcase.UpperCamelCase(k)
		if !optionReflect.FieldByName(k).IsValid() {
			return fmt.Errorf("Unknown config: " + k)
		}
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
		return errors.New("Set was dropped")
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
