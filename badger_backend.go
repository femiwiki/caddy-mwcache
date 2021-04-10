package mwcache

import (
	badger "github.com/dgraph-io/badger/v3"
)

type BadgerBackend struct {
	db *badger.DB
}

func (m *BadgerBackend) get(key string) (string, error) {
	if m.db == nil {
		var err error
		m.db, err = badger.Open(badger.DefaultOptions("").WithInMemory(true))
		if err != nil {
			return "", err
		}
		// TODO research whither explicitly closing is required
		// defer m.db.Close()
	}

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
