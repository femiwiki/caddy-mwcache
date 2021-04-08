package mwcache

import (
	"fmt"

	badger "github.com/dgraph-io/badger/v3"
)

type BadgerBackend struct{}

func (m *BadgerBackend) get(key string) (string, error) {
	db, err := badger.Open(badger.DefaultOptions("").WithInMemory(true))
	if err != nil {
		return "", err
	}
	defer db.Close()

	err = db.View(func(txn *badger.Txn) error {
		item, _ := txn.Get([]byte(key))
		// TODO handle err

		var val []byte

		val, err = item.ValueCopy(nil)
		// TODO handle err
		// TODO use val
		fmt.Printf("Value is %s\n", val)

		return nil
	})
	return "", ErrKeyNotFound
}

func (m *BadgerBackend) set(key string, value string) error {
	db, err := badger.Open(badger.DefaultOptions("").WithInMemory(true))
	if err != nil {
		return err
	}
	defer db.Close()

	err = db.Update(func(txn *badger.Txn) error {
		err := txn.Set([]byte(key), []byte(value))
		return err
	})
	return nil
}

// Interface guards
var (
	_ Backend = (*BadgerBackend)(nil)
)
