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

func (m *MapBackend) put(key string, value string) error {
	// TODO remove oldest cache if full
	m.db[key] = value
	return nil
}

// Interface guards
var (
	_ Backend = (*MapBackend)(nil)
)
