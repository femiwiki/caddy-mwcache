package mwcache

type MapBackend struct {
	db map[string]string
}

func newMapBackend() *MapBackend {
	return &MapBackend{db: make(map[string]string)}
}

func (m *MapBackend) get(key string) (string, error) {
	if val, ok := m.db[key]; ok {
		return val, nil
	}
	return "", ErrKeyNotFound
}

func (m *MapBackend) put(key string, val string) error {
	// TODO remove oldest cache if full
	m.db[key] = val
	return nil
}

func (m *MapBackend) delete(key string) error {
	delete(m.db, key)
	return nil
}

// Interface guards
var (
	_ Backend = (*MapBackend)(nil)
)
