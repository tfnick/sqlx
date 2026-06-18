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

func (c fakeConn) Prepare(string) (driver.Stmt, error) { return nil, nil }
func (c fakeConn) Close() error                        { return nil }
func (c fakeConn) Begin() (driver.Tx, error)           { return &fakeTx{}, nil }
func (c fakeConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, nil
}
func (c fakeConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, nil
}

// fakeTx implements driver.Tx for testing WithTransaction.
type fakeTx struct {
	committed  bool
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

func TestManagerAddAndGetEngine(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	if err := mgr.Add("test", db); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := mgr.GetEngine("test")
	if err != nil {
		t.Fatalf("GetEngine failed: %v", err)
	}
	if got.DB() != db {
		t.Fatal("GetEngine returned Engine for wrong database")
	}
}

func TestManagerGetDB(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	if err := mgr.Add("test", db); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := mgr.GetDB("test")
	if err != nil {
		t.Fatalf("GetDB failed: %v", err)
	}
	if got != db {
		t.Fatal("GetDB returned wrong database")
	}
}

func TestManagerGetDBEmptyNameDefaultsToApp(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	mgr.MustAdd("app", db)

	got, err := mgr.GetDB("")
	if err != nil {
		t.Fatalf("GetDB(\"\") failed: %v", err)
	}
	if got != db {
		t.Fatal("GetDB(\"\") returned wrong database")
	}
}

func TestManagerGetDBMissing(t *testing.T) {
	mgr := NewManager()
	_, err := mgr.GetDB("missing")
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}

func TestManagerMustDBPanic(t *testing.T) {
	mgr := NewManager()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustDB should panic for missing database")
		}
	}()
	mgr.MustDB("missing")
}

func TestManagerStdDB(t *testing.T) {
	mgr := NewManager()
	raw := newFakeDB()
	db := NewDb(raw, "sqlite3")
	defer db.Close()

	mgr.MustAdd("app", db)

	got, err := mgr.StdDB("")
	if err != nil {
		t.Fatalf("StdDB failed: %v", err)
	}
	if got != raw {
		t.Fatal("StdDB returned wrong standard database")
	}
}

func TestManagerDefaultName(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	if err := mgr.Add("app", db); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got, err := mgr.GetEngine("")
	if err != nil {
		t.Fatalf("GetEngine with empty name failed: %v", err)
	}
	if got.DB() != db {
		t.Fatal("GetEngine(\"\") should return the \"app\" database Engine")
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
	_, err := mgr.GetEngine("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}

func TestManagerMustEnginePanic(t *testing.T) {
	mgr := NewManager()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustEngine should panic for missing database")
		}
	}()
	mgr.MustEngine("nonexistent")
}

func TestManagerMustAdd(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	mgr.MustAdd("app", db)
	got := mgr.MustEngine("app")
	if got.DB() != db {
		t.Fatal("MustEngine returned Engine for wrong database")
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

	if mgr.MustEngine("app").DB() != db1 {
		t.Fatal("wrong engine for 'app'")
	}
	if mgr.MustEngine("logs").DB() != db2 {
		t.Fatal("wrong engine for 'logs'")
	}
	if mgr.MustEngine("cache").DB() != db3 {
		t.Fatal("wrong engine for 'cache'")
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

func TestManagerGetEngineEmptyNameDefaultsToApp(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	mgr.MustAdd("app", db)

	// All empty-string variants should resolve to "app"
	got, err := mgr.GetEngine("")
	if err != nil {
		t.Fatalf("GetEngine(\"\") failed: %v", err)
	}
	if got.DB() != db {
		t.Fatal("GetEngine(\"\") returned wrong engine")
	}
}

func TestManagerGetMissingAfterClose(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	mgr.MustAdd("app", db)
	mgr.Close()

	_, err := mgr.GetEngine("app")
	if err == nil {
		t.Fatal("expected error for database after Close")
	}
}
