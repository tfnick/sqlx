package sqlx

import (
	"errors"
	"sync"
)

// Manager manages multiple named database connections.
//
// Example:
//
//	mg := sqlx.NewManager()
//	mg.MustOpen("app", "sqlite3", "app.db")
//	mg.MustOpen("logs", "sqlite3", "logs.db")
//	defer mg.Close()
//
//	// Get the *DB handle and use it directly
//	db := mg.MustDB("app")
//	db.Select(&users, "SELECT * FROM users")
//
//	logDB := mg.MustDB("logs")
//	logDB.Exec("INSERT INTO access_logs ...")
//
//	// Get Engine for dynamic SQL from the DB
//	engine := db.LazyEngine()
//	engine.Select(&users, "SELECT * FROM users WHERE 1=1 #[ AND status=:status ]", params)
type Manager struct {
	databases map[string]*DB
	mu        sync.RWMutex
}

// NewManager creates a new Manager with an empty database registry.
func NewManager() *Manager {
	return &Manager{
		databases: make(map[string]*DB),
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
	return nil
}

// MustAdd is like Add but panics on error.
func (m *Manager) MustAdd(name string, db *DB) {
	if err := m.Add(name, db); err != nil {
		panic(err)
	}
}

// DB returns the database registered under the given name.
// If name is empty, "app" is used.
// Returns an error if the database is not registered.
func (m *Manager) DB(name string) (*DB, error) {
	name = resolveName(name)

	m.mu.RLock()
	db, ok := m.databases[name]
	m.mu.RUnlock()

	if !ok {
		return nil, errors.New("sqlx: database \"" + name + "\" is not registered")
	}
	return db, nil
}

// MustDB is like DB but panics if the database is not registered.
func (m *Manager) MustDB(name string) *DB {
	db, err := m.DB(name)
	if err != nil {
		panic(err)
	}
	return db
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
	}
	return lastErr
}

