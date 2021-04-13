package mwcache

import (
	"fmt"
	"reflect"
	"strconv"

	badger "github.com/dgraph-io/badger/v3"
	cases "github.com/google/agi/core/text/cases"
)

type BadgerBackend struct {
	db *badger.DB
}

func snakeToPascal(snake string) string {
	return cases.Snake(snake).ToPascal()
}

func ValidateBadgerConfig(rawOptions map[string]string) error {
	optionReflect := reflect.ValueOf(badger.Options{})
	for k, _ := range rawOptions {
		k = snakeToPascal(k)
		if !optionReflect.FieldByName(k).IsValid() {
			return fmt.Errorf("Unknown config: " + k)
		}
	}
	return nil
}

func parseOptions(rawOptions map[string]string) (*badger.Options, error) {
	o := badger.DefaultOptions("")
	optionsReflect := reflect.ValueOf(&o)
	for k, strV := range rawOptions {
		k = snakeToPascal(k)
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

	return &o, nil
}

func newBadgerBackend(rawOptions map[string]string) (*BadgerBackend, error) {
	opt, err := parseOptions(rawOptions)
	if err != nil {
		return nil, err
	}
	db, err := badger.Open(*opt)
	if err != nil {
		return nil, err
	}

	// TODO research whither explicitly closing (m.db.Close()) is required
	return &BadgerBackend{db: db}, nil
}

func (m *BadgerBackend) get(key string) (string, error) {
	txn := m.db.NewTransaction(false)
	defer txn.Discard()
	item, err := txn.Get([]byte(key))
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return "", ErrKeyNotFound
		}
		return "", err
	}
	var val []byte
	val, err = item.ValueCopy(nil)

	if err != nil {
		return "", err
	}
	return string(val), err
}

func (m *BadgerBackend) put(key string, val string) error {
	if m.db == nil {
		var err error
		m.db, err = badger.Open(badger.DefaultOptions("").WithInMemory(true))
		if err != nil {
			return err
		}
		// TODO research whither explicitly closing is required
		// defer m.db.Close()
	}

	txn := m.db.NewTransaction(true)
	defer txn.Discard()

	if err := txn.Set([]byte(key), []byte(val)); err != nil {
		return err
	}
	if err := txn.Commit(); err != nil {
		return err
	}
	return nil
}

func (m *BadgerBackend) delete(key string) error {
	if m.db == nil {
		var err error
		m.db, err = badger.Open(badger.DefaultOptions("").WithInMemory(true))
		if err != nil {
			return err
		}
		// TODO research whither explicitly closing is required
		// defer m.db.Close()
	}

	txn := m.db.NewTransaction(true)
	defer txn.Discard()

	if err := txn.Delete([]byte(key)); err != nil {
		return err
	}
	if err := txn.Commit(); err != nil {
		return err
	}
	return nil
}

// Interface guards
var (
	_ Backend = (*BadgerBackend)(nil)
)
