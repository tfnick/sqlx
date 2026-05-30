package sqlx

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
)

// fakeConnector implements driver.Connector for testing without a real database.
type fakeConnector struct{}

func (f fakeConnector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{}, nil }
func (f fakeConnector) Driver() driver.Driver                        { return &fakeDriver{} }

type fakeDriver struct{}

func (d fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{}, nil }

type fakeConn struct{}

func (c fakeConn) Prepare(string) (driver.Stmt, error)     { return nil, nil }
func (c fakeConn) Close() error                            { return nil }
func (c fakeConn) Begin() (driver.Tx, error)               { return &fakeTx{}, nil }
func (c fakeConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, nil
}
func (c fakeConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, nil
}

// fakeTx implements driver.Tx for testing WithTransaction.
type fakeTx struct {
	committed bool
	rolledBack bool
}

func (t *fakeTx) Commit() error   { t.committed = true; return nil }
func (t *fakeTx) Rollback() error { t.rolledBack = true; return nil }

func newFakeDB() *sql.DB {
	return sql.OpenDB(fakeConnector{})
}

func TestNewManager(t *testing.T) {
	mgr := NewManager()
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
	if len(mgr.databases) != 0 {
		t.Fatal("new manager should have no databases")
	}
}

func TestManagerAddAndGet(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	if err := mgr.Add("test", db); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := mgr.DB("test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != db {
		t.Fatal("Get returned wrong database")
	}
}

func TestManagerDefaultName(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	if err := mgr.Add("app", db); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := mgr.DB("")
	if err != nil {
		t.Fatalf("Get with empty name failed: %v", err)
	}
	if got != db {
		t.Fatal("Get(\"\") should return the \"app\" database")
	}
}

func TestManagerDuplicateName(t *testing.T) {
	mgr := NewManager()
	db1 := NewDb(newFakeDB(), "sqlite3")
	defer db1.Close()
	db2 := NewDb(newFakeDB(), "sqlite3")
	defer db2.Close()

	if err := mgr.Add("app", db1); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	if err := mgr.Add("app", db2); err == nil {
		// The second Add should fail; close db2 since Manager didn't take ownership
		t.Fatal("expected error for duplicate database name")
	}
}

func TestManagerGetMissing(t *testing.T) {
	mgr := NewManager()
	_, err := mgr.DB("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}

func TestManagerMustGetPanic(t *testing.T) {
	mgr := NewManager()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustGet should panic for missing database")
		}
	}()
	mgr.MustDB("nonexistent")
}

func TestManagerMustAdd(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	mgr.MustAdd("app", db)
	got := mgr.MustDB("app")
	if got != db {
		t.Fatal("MustGet returned wrong database")
	}
}

func TestManagerMustAddDuplicate(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	mgr.MustAdd("app", db)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustAdd should panic for duplicate database")
		}
	}()
	mgr.MustAdd("app", db)
}

func TestManagerClose(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	mgr.MustAdd("app", db)

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if len(mgr.databases) != 0 {
		t.Fatal("Close should clear all databases")
	}
}

func TestDBLazyEngine(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	engine := db.LazyEngine()
	if engine == nil {
		t.Fatal("LazyEngine returned nil")
	}

	// Second call should return the same instance
	engine2 := db.LazyEngine()
	if engine != engine2 {
		t.Fatal("LazyEngine should return the same Engine instance")
	}
}

func TestResolveName(t *testing.T) {
	if got := resolveName(""); got != "app" {
		t.Fatalf("resolveName(\"\") = %q, want \"app\"", got)
	}
	if got := resolveName("custom"); got != "custom" {
		t.Fatalf("resolveName(\"custom\") = %q, want \"custom\"", got)
	}
}

func TestWithTransactionSuccess(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	var called bool
	err := db.WithTransaction(func(tx *Tx) error {
		called = true
		if tx == nil {
			t.Fatal("expected non-nil Tx")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTransaction failed: %v", err)
	}
	if !called {
		t.Fatal("fn was not called")
	}
}

func TestWithTransactionErrorRollback(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	testErr := sql.ErrNoRows
	err := db.WithTransaction(func(tx *Tx) error {
		return testErr
	})
	if err != testErr {
		t.Fatalf("expected %v, got %v", testErr, err)
	}
}

func TestWithTransactionPanicRollback(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	panicVal := "test panic"
	var recovered interface{}
	func() {
		defer func() {
			recovered = recover()
		}()
		db.WithTransaction(func(tx *Tx) error {
			panic(panicVal)
		})
	}()
	if recovered != panicVal {
		t.Fatalf("expected panic %v, got %v", panicVal, recovered)
	}
}

func TestManagerMultipleDatabases(t *testing.T) {
	mgr := NewManager()
	db1 := NewDb(newFakeDB(), "sqlite3")
	db2 := NewDb(newFakeDB(), "sqlite3")
	db3 := NewDb(newFakeDB(), "sqlite3")
	defer db1.Close()
	defer db2.Close()
	defer db3.Close()

	mgr.MustAdd("app", db1)
	mgr.MustAdd("logs", db2)
	mgr.MustAdd("cache", db3)

	if mgr.MustDB("app") != db1 {
		t.Fatal("wrong db for 'app'")
	}
	if mgr.MustDB("logs") != db2 {
		t.Fatal("wrong db for 'logs'")
	}
	if mgr.MustDB("cache") != db3 {
		t.Fatal("wrong db for 'cache'")
	}
}

func TestManagerCloseMultiple(t *testing.T) {
	mgr := NewManager()
	db1 := NewDb(newFakeDB(), "sqlite3")
	db2 := NewDb(newFakeDB(), "sqlite3")
	mgr.MustAdd("db1", db1)
	mgr.MustAdd("db2", db2)

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if len(mgr.databases) != 0 {
		t.Fatal("Close should clear all databases")
	}
}

func TestManagerCloseEmpty(t *testing.T) {
	mgr := NewManager()
	if err := mgr.Close(); err != nil {
		t.Fatalf("Close on empty manager failed: %v", err)
	}
}

func TestDBLazyEngineAfterUnsafe(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	eng1 := db.LazyEngine()
	eng2 := db.Unsafe().LazyEngine()

	if eng1 == eng2 {
		t.Fatal("Unsafe() copy should get its own engine")
	}
	if eng2 == nil {
		t.Fatal("LazyEngine on Unsafe copy returned nil")
	}
}

func TestDBLazyEngineSameInstance(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	eng1 := db.LazyEngine()
	eng2 := db.LazyEngine()
	eng3 := db.LazyEngine()

	if eng1 != eng2 || eng2 != eng3 {
		t.Fatal("multiple LazyEngine calls should return the same instance")
	}
}

func TestManagerGetEmptyNameDefaultsToApp(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	mgr.MustAdd("app", db)

	// All empty-string variants should resolve to "app"
	got, err := mgr.DB("")
	if err != nil {
		t.Fatalf("Get(\"\") failed: %v", err)
	}
	if got != db {
		t.Fatal("Get(\"\") returned wrong db")
	}
}

func TestManagerGetMissingAfterClose(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	mgr.MustAdd("app", db)
	mgr.Close()

	_, err := mgr.DB("app")
	if err == nil {
		t.Fatal("expected error for database after Close")
	}
}

