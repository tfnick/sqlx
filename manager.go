package sqlx

import (
	"database/sql"
	"errors"
	"sync"
)

// Manager manages multiple named database connections.
//
// Shortcut methods (Get, Select, Exec, Queryx, QueryRowx) operate on the
// default "app" database using named parameters (:name) and dynamic SQL
// (#[ ] blocks). For non-default databases, use MustEngine("name") first.
//
// Example:
//
//	mg := sqlx.NewManager()
//	mg.MustOpen("app", "sqlite3", "app.db")
//	mg.MustOpen("logs", "sqlite3", "logs.db")
//	defer mg.Close()
//
//	// Shortcut: default "app" database with named params + dynamic SQL
//	var account Account
//	mg.Get(&account, "SELECT * FROM accounts WHERE account_id = :id",
//	    map[string]interface{}{"id": userID})
//
//	// Direct DB access for ? placeholder queries
//	db := mg.MustDB("app")
//	db.Select(&users, "SELECT * FROM users")
//
//	// Engine access for non-default databases
//	eng := mg.MustEngine("logs")
//	eng.Exec("INSERT INTO access_logs (action) VALUES (:action)",
//	    map[string]interface{}{"action": "login"})
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

// Engine returns the Engine for the named database, creating one lazily if needed.
// This is equivalent to m.DB(name).LazyEngine() but in a single call.
// If name is empty, "app" is used.
func (m *Manager) Engine(name string) (*Engine, error) {
	db, err := m.DB(name)
	if err != nil {
		return nil, err
	}
	return db.LazyEngine(), nil
}

// MustEngine is like Engine but panics if the database is not registered.
func (m *Manager) MustEngine(name string) *Engine {
	eng, err := m.Engine(name)
	if err != nil {
		panic(err)
	}
	return eng
}

// hasNamedParams returns true if the query should be routed to Engine
// (contains :param_name patterns or #[ ] dynamic SQL blocks).
func hasNamedParams(query string) bool {
	return namedParamRegex.MatchString(query) || preprocessRegex.MatchString(query)
}

// Get retrieves a single row from the default database.
// Auto-routes to Engine (named params + dynamic SQL) if the query contains
// :param patterns, otherwise uses DB directly (? placeholders).
//
// Example (named params):
//
//	var account Account
//	err := mgr.Get(&account, "SELECT * FROM accounts WHERE account_id = :id",
//	    map[string]interface{}{"id": userID})
//
// Example (positional params):
//
//	var account Account
//	err := mgr.Get(&account, "SELECT * FROM accounts WHERE id = ?", userID)
func (m *Manager) Get(dest interface{}, query string, args ...interface{}) error {
	if hasNamedParams(query) {
		return m.MustEngine("").Get(dest, query, args...)
	}
	return m.MustDB("").Get(dest, query, args...)
}

// Select retrieves multiple rows from the default database.
// Auto-routes to Engine (named params + dynamic SQL) if the query contains
// :param patterns, otherwise uses DB directly (? placeholders).
// dest must be a pointer to a slice.
//
// Example (named params):
//
//	var users []User
//	err := mgr.Select(&users, "SELECT * FROM users WHERE status = :status",
//	    map[string]interface{}{"status": "active"})
//
// Example (positional params):
//
//	var users []User
//	err := mgr.Select(&users, "SELECT * FROM users WHERE status = ?", "active")
func (m *Manager) Select(dest interface{}, query string, args ...interface{}) error {
	if hasNamedParams(query) {
		return m.MustEngine("").Select(dest, query, args...)
	}
	return m.MustDB("").Select(dest, query, args...)
}

// Exec executes a query on the default database.
// Auto-routes to Engine (named params + dynamic SQL) if the query contains
// :param patterns, otherwise uses DB directly (? placeholders).
//
// Example (named params):
//
//	result, err := mgr.Exec("INSERT INTO users (name) VALUES (:name)",
//	    map[string]interface{}{"name": "Alice"})
//
// Example (positional params):
//
//	result, err := mgr.Exec("INSERT INTO users (name) VALUES (?)", "Alice")
func (m *Manager) Exec(query string, args ...interface{}) (sql.Result, error) {
	if hasNamedParams(query) {
		return m.MustEngine("").Exec(query, args...)
	}
	return m.MustDB("").Exec(query, args...)
}

// Queryx executes a query and returns *Rows from the default database.
// Auto-routes to Engine (named params + dynamic SQL) if the query contains
// :param patterns, otherwise uses DB directly (? placeholders).
func (m *Manager) Queryx(query string, args ...interface{}) (*Rows, error) {
	if hasNamedParams(query) {
		return m.MustEngine("").Queryx(query, args...)
	}
	return m.MustDB("").Queryx(query, args...)
}

// QueryRowx executes a query and returns *Row from the default database.
// Auto-routes to Engine (named params + dynamic SQL) if the query contains
// :param patterns, otherwise uses DB directly (? placeholders).
func (m *Manager) QueryRowx(query string, args ...interface{}) *Row {
	if hasNamedParams(query) {
		return m.MustEngine("").QueryRowx(query, args...)
	}
	return m.MustDB("").QueryRowx(query, args...)
}

// WithTransaction executes fn within a transaction on the default database.
// Auto-commits if fn returns nil, rolls back if fn returns an error or panics.
//
// Example:
//
//	err := mgr.WithTransaction(func(tx *sqlx.Tx) error {
//	    _, err := tx.Exec("INSERT INTO users (name) VALUES (?)", "Alice")
//	    return err
//	})
func (m *Manager) WithTransaction(fn func(*Tx) error) error {
	return m.MustDB("").WithTransaction(fn)
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

