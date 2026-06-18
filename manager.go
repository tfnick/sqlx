package sqlx

import (
	"errors"
	"sync"
)

// Manager manages multiple named database connections and exposes them through
// cached Engine instances.
type Manager struct {
	databases map[string]*DB
	engines   map[string]*Engine
	mu        sync.RWMutex
}

// NewManager creates a new Manager with an empty database registry.
func NewManager() *Manager {
	return &Manager{
		databases: make(map[string]*DB),
		engines:   make(map[string]*Engine),
	}
}

// defaultName is the default database name when an empty string is provided.
const defaultName = "app"

// resolveName returns the effective database name, defaulting to "app".
func resolveName(name string) string {
	if name == "" {
		return defaultName
	}
	return name
}

// Open opens a new database connection and registers it under the given name.
// If name is empty, "app" is used.
func (m *Manager) Open(name, driverName, dataSourceName string) error {
	name = resolveName(name)

	db, err := Open(driverName, dataSourceName)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.databases[name]; exists {
		db.Close()
		return errors.New("sqlx: database with name \"" + name + "\" already registered")
	}

	m.databases[name] = db
	delete(m.engines, name)
	return nil
}

// MustOpen is like Open but panics on error.
func (m *Manager) MustOpen(name, driverName, dataSourceName string) {
	if err := m.Open(name, driverName, dataSourceName); err != nil {
		panic(err)
	}
}

// Add registers an existing *DB under the given name.
// If name is empty, "app" is used.
func (m *Manager) Add(name string, db *DB) error {
	name = resolveName(name)

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.databases[name]; exists {
		return errors.New("sqlx: database with name \"" + name + "\" already registered")
	}

	m.databases[name] = db
	delete(m.engines, name)
	return nil
}

// MustAdd is like Add but panics on error.
func (m *Manager) MustAdd(name string, db *DB) {
	if err := m.Add(name, db); err != nil {
		panic(err)
	}
}

// GetEngine returns the Engine for the database registered under name.
// If name is empty, "app" is used. Engines are cached per database name.
func (m *Manager) GetEngine(name string) (*Engine, error) {
	name = resolveName(name)

	m.mu.RLock()
	if engine, ok := m.engines[name]; ok {
		m.mu.RUnlock()
		return engine, nil
	}
	db, ok := m.databases[name]
	m.mu.RUnlock()

	if !ok {
		return nil, errors.New("sqlx: database \"" + name + "\" is not registered")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if engine, ok := m.engines[name]; ok {
		return engine, nil
	}
	engine := NewEngine(db)
	m.engines[name] = engine
	return engine, nil
}

// MustEngine is like GetEngine but panics if the database is not registered.
func (m *Manager) MustEngine(name string) *Engine {
	engine, err := m.GetEngine(name)
	if err != nil {
		panic(err)
	}
	return engine
}

// Engine is a short panic-on-missing alias for MustEngine.
func (m *Manager) Engine(name string) *Engine {
	return m.MustEngine(name)
}

// DefaultEngine returns the Engine for the default database.
func (m *Manager) DefaultEngine() *Engine {
	return m.MustEngine("")
}

// Close closes all registered databases and clears the registry.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for name, db := range m.databases {
		if err := db.Close(); err != nil {
			lastErr = err
		}
		delete(m.databases, name)
		delete(m.engines, name)
	}
	return lastErr
}
