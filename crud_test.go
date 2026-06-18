package sqlx

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

type crudUser struct {
	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Email  string `db:"email"`
	Age    int    `db:"age"`
	Status string `db:"status"`
}

type crudParams map[string]interface{}

func withCrudSQLite(t *testing.T, fn func(*DB)) {
	raw, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open modernc sqlite: %v", err)
	}
	db := NewDb(raw, "sqlite")
	defer db.Close()

	MultiExec(db, `
DROP TABLE IF EXISTS crud_users;
CREATE TABLE crud_users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT,
	email TEXT UNIQUE,
	age INTEGER,
	status TEXT
);
`)
	defer MultiExec(db, `DROP TABLE IF EXISTS crud_users;`)
	fn(db)
}

func TestManagerGetEngine(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	if err := mgr.Add("app", db); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	e1, err := mgr.GetEngine("")
	if err != nil {
		t.Fatalf("GetEngine failed: %v", err)
	}
	e2 := mgr.MustEngine("app")
	if e1 != e2 {
		t.Fatal("GetEngine should cache engines per database")
	}
	if e1.DB() != db {
		t.Fatal("engine should be bound to the registered DB")
	}

	if mgr.DefaultEngine() != e1 {
		t.Fatal("DefaultEngine should return the default cached engine")
	}
}

func TestManagerGetEngineConcurrent(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	if err := mgr.Add("app", db); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	const workers = 32
	var wg sync.WaitGroup
	engines := make([]*Engine, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			engines[i], errs[i] = mgr.GetEngine("app")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("GetEngine[%d] failed: %v", i, err)
		}
	}
	for i := 1; i < workers; i++ {
		if engines[i] != engines[0] {
			t.Fatal("concurrent GetEngine should return the cached instance")
		}
	}
}

func TestManagerCloseClearsEngineCache(t *testing.T) {
	mgr := NewManager()
	db := NewDb(newFakeDB(), "sqlite3")

	mgr.MustAdd("app", db)
	if _, err := mgr.GetEngine("app"); err != nil {
		t.Fatalf("GetEngine failed: %v", err)
	}
	if len(mgr.engines) != 1 {
		t.Fatalf("expected cached engine, got %d", len(mgr.engines))
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if len(mgr.engines) != 0 {
		t.Fatal("Close should clear engine cache")
	}
}

func TestEngineCRUDHelpersSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)

		var id int64
		err := e.InsertReturning(&id, "crud_users", crudUser{
			Name:   "Tom",
			Email:  "tom@example.com",
			Age:    20,
			Status: "active",
		}, Columns("name", "email", "age", "status"), Returning("id"))
		if err != nil {
			t.Fatalf("InsertReturning failed: %v", err)
		}
		if id == 0 {
			t.Fatal("expected inserted id")
		}

		_, err = e.Update("crud_users", crudUser{
			ID:     id,
			Name:   "Tommy",
			Email:  "tom@example.com",
			Age:    21,
			Status: "active",
		}, Keys("id"), Columns("name", "email", "age"))
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		var got crudUser
		err = e.GetBy(&got, "crud_users",
			Where("id", id),
			Columns("id", "name", "email", "age", "status"),
		)
		if err != nil {
			t.Fatalf("GetBy failed: %v", err)
		}
		if got.Name != "Tommy" || got.Age != 21 {
			t.Fatalf("unexpected user: %#v", got)
		}

		var users []crudUser
		err = e.SelectBy(&users, "crud_users",
			Where("status", "active"),
			Columns("id", "name", "email", "age", "status"),
			OrderBy("id DESC"),
			LimitOffset(10, 0),
		)
		if err != nil {
			t.Fatalf("SelectBy failed: %v", err)
		}
		if len(users) != 1 {
			t.Fatalf("expected 1 user, got %d", len(users))
		}
	})
}

func TestEngineBaseSQLAPIsSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		if _, err := e.ExecP(`
			INSERT INTO crud_users (name, email, age, status)
			VALUES (?, ?, ?, ?)
		`, "P", "p@example.com", 10, "active"); err != nil {
			t.Fatalf("ExecP failed: %v", err)
		}

		var got crudUser
		if err := e.GetP(&got, `
			SELECT id, name, email, age, status
			FROM crud_users
			WHERE email = ?
		`, "p@example.com"); err != nil {
			t.Fatalf("GetP failed: %v", err)
		}
		if got.Name != "P" {
			t.Fatalf("unexpected GetP result: %#v", got)
		}

		if _, err := e.ExecNamed(`
			INSERT INTO crud_users (name, email, age, status)
			VALUES (:name, :email, :age, :status)
		`, map[string]interface{}{
			"name":   "N",
			"email":  "n@example.com",
			"age":    11,
			"status": "inactive",
		}); err != nil {
			t.Fatalf("ExecNamed failed: %v", err)
		}

		var named crudUser
		if err := e.GetNamed(&named, `
			SELECT id, name, email, age, status
			FROM crud_users
			WHERE email = :email
		`, map[string]interface{}{"email": "n@example.com"}); err != nil {
			t.Fatalf("GetNamed failed: %v", err)
		}
		if named.Name != "N" {
			t.Fatalf("unexpected GetNamed result: %#v", named)
		}

		var active []crudUser
		if err := e.Select(&active, `
			SELECT id, name, email, age, status
			FROM crud_users
			WHERE 1=1
			#[ AND status = :status ]
			#[ AND age IN :ages ]
			ORDER BY id
		`, map[string]interface{}{
			"status": "active",
			"ages":   []int{10},
		}); err != nil {
			t.Fatalf("dynamic Select failed: %v", err)
		}
		if len(active) != 1 || active[0].Email != "p@example.com" {
			t.Fatalf("unexpected dynamic Select result: %#v", active)
		}
	})
}

func TestEngineContextAPIsSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		ctx := context.Background()
		if _, err := e.ExecPContext(ctx, `
			INSERT INTO crud_users (name, email, age, status)
			VALUES (?, ?, ?, ?)
		`, "C", "c@example.com", 12, "active"); err != nil {
			t.Fatalf("ExecPContext failed: %v", err)
		}

		var got crudUser
		if err := e.GetNamedContext(ctx, &got, `
			SELECT id, name, email, age, status
			FROM crud_users
			WHERE email = :email
		`, map[string]interface{}{"email": "c@example.com"}); err != nil {
			t.Fatalf("GetNamedContext failed: %v", err)
		}
		if got.Name != "C" {
			t.Fatalf("unexpected context result: %#v", got)
		}

		var users []crudUser
		if err := e.SelectContext(ctx, &users, `
			SELECT id, name, email, age, status
			FROM crud_users
			WHERE 1=1
			#[ AND status = :status ]
		`, map[string]interface{}{"status": "active"}); err != nil {
			t.Fatalf("SelectContext failed: %v", err)
		}
		if len(users) != 1 {
			t.Fatalf("expected 1 user, got %d", len(users))
		}
	})
}

func TestEngineCRUDContextAPIsSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		ctx := context.Background()
		var id int64
		if err := e.InsertReturningContext(ctx, &id, "crud_users", crudUser{
			Name:   "CrudCtx",
			Email:  "crudctx@example.com",
			Age:    13,
			Status: "active",
		}, Columns("name", "email", "age", "status"), Returning("id")); err != nil {
			t.Fatalf("InsertReturningContext failed: %v", err)
		}

		if _, err := e.UpdateContext(ctx, "crud_users", crudUser{
			ID:     id,
			Name:   "CrudCtx2",
			Email:  "crudctx@example.com",
			Age:    14,
			Status: "active",
		}, Keys("id"), Columns("name", "age")); err != nil {
			t.Fatalf("UpdateContext failed: %v", err)
		}

		var got crudUser
		if err := e.GetByContext(ctx, &got, "crud_users", Where("id", id)); err != nil {
			t.Fatalf("GetByContext failed: %v", err)
		}
		if got.Name != "CrudCtx2" || got.Age != 14 {
			t.Fatalf("unexpected CRUD context result: %#v", got)
		}

		if _, err := e.DeleteContext(ctx, "crud_users", Where("id", id)); err != nil {
			t.Fatalf("DeleteContext failed: %v", err)
		}
		if err := e.GetByContext(ctx, &got, "crud_users", Where("id", id)); err != sql.ErrNoRows {
			t.Fatalf("expected deleted row to be gone, got %v", err)
		}
	})
}

func TestEngineBatchAndSaveSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		users := []crudUser{
			{Name: "A", Email: "a@example.com", Age: 10, Status: "active"},
			{Name: "B", Email: "b@example.com", Age: 11, Status: "active"},
		}
		if _, err := e.InsertMany("crud_users", users, Columns("name", "email", "age", "status")); err != nil {
			t.Fatalf("InsertMany failed: %v", err)
		}

		users[0].Name = "A2"
		users[0].Age = 12
		if _, err := e.SaveMany("crud_users", users,
			ConflictKeys("email"),
			InsertColumns("name", "email", "age", "status"),
			UpdateColumns("name", "age", "status"),
		); err != nil {
			t.Fatalf("SaveMany failed: %v", err)
		}

		var got crudUser
		if err := e.GetBy(&got, "crud_users", Where("email", "a@example.com")); err != nil {
			t.Fatalf("GetBy after SaveMany failed: %v", err)
		}
		if got.Name != "A2" || got.Age != 12 {
			t.Fatalf("SaveMany did not update row: %#v", got)
		}
	})
}

func TestEngineTransactionSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		err := e.WithTransaction(func(tx *Engine) error {
			_, err := tx.Insert("crud_users", crudUser{
				Name:   "Tx",
				Email:  "tx@example.com",
				Age:    30,
				Status: "active",
			}, Columns("name", "email", "age", "status"))
			return err
		})
		if err != nil {
			t.Fatalf("WithTransaction failed: %v", err)
		}

		var got crudUser
		if err := e.GetBy(&got, "crud_users", Where("email", "tx@example.com")); err != nil {
			t.Fatalf("transaction insert not visible: %v", err)
		}
	})
}

func TestEngineTransactionBaseAPIsSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		err := e.WithTransaction(func(tx *Engine) error {
			if _, err := tx.ExecNamed(`
				INSERT INTO crud_users (name, email, age, status)
				VALUES (:name, :email, :age, :status)
			`, crudUser{Name: "TxBase", Email: "txbase@example.com", Age: 22, Status: "active"}); err != nil {
				return err
			}

			var got crudUser
			if err := tx.Get(&got, `
				SELECT id, name, email, age, status
				FROM crud_users
				WHERE 1=1
				#[ AND email = :email ]
			`, map[string]interface{}{"email": "txbase@example.com"}); err != nil {
				return err
			}
			if got.Name != "TxBase" {
				t.Fatalf("unexpected tx result: %#v", got)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("transaction base APIs failed: %v", err)
		}
	})
}

func TestEngineTransactionRollbackSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		wantErr := errors.New("stop")
		err := e.WithTransaction(func(tx *Engine) error {
			if _, err := tx.Insert("crud_users", crudUser{
				Name:   "Rollback",
				Email:  "rollback@example.com",
				Age:    30,
				Status: "active",
			}, Columns("name", "email", "age", "status")); err != nil {
				return err
			}
			return wantErr
		})
		if err != wantErr {
			t.Fatalf("expected rollback error %v, got %v", wantErr, err)
		}

		var got crudUser
		err = e.GetBy(&got, "crud_users", Where("email", "rollback@example.com"))
		if err != sql.ErrNoRows {
			t.Fatalf("expected sql.ErrNoRows after rollback, got %v", err)
		}
	})
}

func TestEngineNestedTransactionErrorSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		err := e.WithTransaction(func(tx *Engine) error {
			return tx.WithTransaction(func(nested *Engine) error {
				return nil
			})
		})
		if err == nil {
			t.Fatal("expected nested transaction error")
		}
	})
}

func TestPreparedCRUDSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		stmt, err := e.PrepareInsert("crud_users", Columns("name", "email", "age", "status"))
		if err != nil {
			t.Fatalf("PrepareInsert failed: %v", err)
		}
		defer stmt.Close()

		if _, err := stmt.ExecMany([]crudUser{
			{Name: "P1", Email: "p1@example.com", Age: 1, Status: "active"},
			{Name: "P2", Email: "p2@example.com", Age: 2, Status: "active"},
		}); err != nil {
			t.Fatalf("prepared ExecMany failed: %v", err)
		}

		var users []crudUser
		if err := e.SelectBy(&users, "crud_users", Where("status", "active")); err != nil {
			t.Fatalf("SelectBy failed: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("expected 2 users, got %d", len(users))
		}
	})
}

func TestPreparedCRUDCachesStructTraversalsSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		stmt, err := e.PrepareInsert("crud_users", Columns("name", "email", "age", "status"))
		if err != nil {
			t.Fatalf("PrepareInsert failed: %v", err)
		}
		defer stmt.Close()

		for i, user := range []crudUser{
			{Name: "Cache1", Email: "cache1@example.com", Age: 1, Status: "active"},
			{Name: "Cache2", Email: "cache2@example.com", Age: 2, Status: "active"},
		} {
			if _, err := stmt.Exec(user); err != nil {
				t.Fatalf("prepared Exec[%d] failed: %v", i, err)
			}
		}

		if _, ok := stmt.binder.traversals.Load(reflect.TypeOf(crudUser{})); !ok {
			t.Fatal("expected prepared CRUD statement to cache struct traversals")
		}
	})
}

func TestPreparedSaveSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		stmt, err := e.PrepareSave("crud_users",
			ConflictKeys("email"),
			InsertColumns("name", "email", "age", "status"),
			UpdateColumns("name", "age", "status"),
		)
		if err != nil {
			t.Fatalf("PrepareSave failed: %v", err)
		}
		defer stmt.Close()

		if _, err := stmt.ExecMany([]crudUser{
			{Name: "S1", Email: "s1@example.com", Age: 1, Status: "active"},
			{Name: "S2", Email: "s2@example.com", Age: 2, Status: "active"},
		}); err != nil {
			t.Fatalf("prepared save insert failed: %v", err)
		}
		if _, err := stmt.Exec(crudUser{Name: "S1b", Email: "s1@example.com", Age: 3, Status: "active"}); err != nil {
			t.Fatalf("prepared save update failed: %v", err)
		}

		var got crudUser
		if err := e.GetBy(&got, "crud_users", Where("email", "s1@example.com")); err != nil {
			t.Fatalf("GetBy failed: %v", err)
		}
		if got.Name != "S1b" || got.Age != 3 {
			t.Fatalf("prepared save did not update: %#v", got)
		}
	})
}

func TestPreparedCRUDContextSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		ctx := context.Background()
		insert, err := e.PrepareInsertContext(ctx, "crud_users",
			Columns("name", "email", "age", "status"),
			Returning("id"),
		)
		if err != nil {
			t.Fatalf("PrepareInsertContext failed: %v", err)
		}
		defer insert.Close()

		var id int64
		if err := insert.ExecReturningContext(ctx, &id, crudUser{
			Name:   "PreparedCtx",
			Email:  "preparedctx@example.com",
			Age:    9,
			Status: "active",
		}); err != nil {
			t.Fatalf("ExecReturningContext failed: %v", err)
		}
		if id == 0 {
			t.Fatal("expected returned id")
		}

		save, err := e.PrepareSaveContext(ctx, "crud_users",
			ConflictKeys("email"),
			InsertColumns("name", "email", "age", "status"),
			UpdateColumns("name", "age", "status"),
		)
		if err != nil {
			t.Fatalf("PrepareSaveContext failed: %v", err)
		}
		defer save.Close()

		if _, err := save.ExecManyContext(ctx, []crudUser{
			{Name: "PreparedCtx2", Email: "preparedctx@example.com", Age: 10, Status: "active"},
			{Name: "PreparedCtx3", Email: "preparedctx3@example.com", Age: 11, Status: "active"},
		}); err != nil {
			t.Fatalf("ExecManyContext failed: %v", err)
		}
		var users []crudUser
		if err := e.SelectBy(&users, "crud_users", Where("status", "active")); err != nil {
			t.Fatalf("SelectBy failed: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("expected 2 users after prepared context save, got %d", len(users))
		}
	})
}

func TestEngineInsertManyReturningSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		var ids []int64
		err := e.InsertManyReturning(&ids, "crud_users", []crudUser{
			{Name: "R1", Email: "r1@example.com", Age: 1, Status: "active"},
			{Name: "R2", Email: "r2@example.com", Age: 2, Status: "active"},
		}, Columns("name", "email", "age", "status"), Returning("id"))
		if err != nil {
			t.Fatalf("InsertManyReturning failed: %v", err)
		}
		if len(ids) != 2 || ids[0] == 0 || ids[1] == 0 {
			t.Fatalf("unexpected returned ids: %#v", ids)
		}
	})
}

func TestEngineInsertReturningNonIDColumnSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		var email string
		err := e.InsertReturning(&email, "crud_users", crudUser{
			Name:   "ReturnEmail",
			Email:  "return-email@example.com",
			Age:    1,
			Status: "active",
		}, Columns("name", "email", "age", "status"), Returning("email"))
		if err != nil {
			t.Fatalf("InsertReturning email failed: %v", err)
		}
		if email != "return-email@example.com" {
			t.Fatalf("unexpected returned email: %q", email)
		}
	})
}

func TestEngineBatchSizeChunkingSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		rows := []crudUser{
			{Name: "C1", Email: "c1@example.com", Age: 1, Status: "active"},
			{Name: "C2", Email: "c2@example.com", Age: 2, Status: "active"},
			{Name: "C3", Email: "c3@example.com", Age: 3, Status: "active"},
			{Name: "C4", Email: "c4@example.com", Age: 4, Status: "active"},
			{Name: "C5", Email: "c5@example.com", Age: 5, Status: "active"},
		}
		if _, err := e.InsertMany("crud_users", rows, Columns("name", "email", "age", "status"), BatchSize(2)); err != nil {
			t.Fatalf("chunked InsertMany failed: %v", err)
		}

		var users []crudUser
		if err := e.SelectBy(&users, "crud_users", Where("status", "active")); err != nil {
			t.Fatalf("SelectBy failed: %v", err)
		}
		if len(users) != 5 {
			t.Fatalf("expected 5 users, got %d", len(users))
		}

		for i := range rows {
			rows[i].Name += "x"
		}
		if _, err := e.SaveMany("crud_users", rows,
			ConflictKeys("email"),
			InsertColumns("name", "email", "age", "status"),
			UpdateColumns("name", "age", "status"),
			BatchSize(2),
		); err != nil {
			t.Fatalf("chunked SaveMany failed: %v", err)
		}

		var got crudUser
		if err := e.GetBy(&got, "crud_users", Where("email", "c5@example.com")); err != nil {
			t.Fatalf("GetBy after SaveMany failed: %v", err)
		}
		if got.Name != "C5x" {
			t.Fatalf("expected chunked save update, got %#v", got)
		}
	})
}

func TestEngineInsertManyReturningChunkingSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		var ids []int64
		err := e.InsertManyReturning(&ids, "crud_users", []crudUser{
			{Name: "RC1", Email: "rc1@example.com", Age: 1, Status: "active"},
			{Name: "RC2", Email: "rc2@example.com", Age: 2, Status: "active"},
			{Name: "RC3", Email: "rc3@example.com", Age: 3, Status: "active"},
			{Name: "RC4", Email: "rc4@example.com", Age: 4, Status: "active"},
			{Name: "RC5", Email: "rc5@example.com", Age: 5, Status: "active"},
		}, Columns("name", "email", "age", "status"), Returning("id"), BatchSize(2))
		if err != nil {
			t.Fatalf("chunked InsertManyReturning failed: %v", err)
		}
		if len(ids) != 5 {
			t.Fatalf("expected 5 ids, got %d: %#v", len(ids), ids)
		}
		for _, id := range ids {
			if id == 0 {
				t.Fatalf("unexpected zero id in %#v", ids)
			}
		}
	})
}

func TestEngineCRUDErrors(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		if _, err := e.Insert("bad table", crudUser{}, Columns("name")); err == nil {
			t.Fatal("expected invalid identifier error")
		}
		if _, err := e.Insert("crud_users", crudUser{}); err == nil {
			t.Fatal("expected missing columns error")
		}
		if _, err := e.InsertMany("crud_users", []crudUser{}, Columns("name")); err == nil {
			t.Fatal("expected empty batch error")
		}
		if _, err := e.Update("crud_users", crudUser{}, Columns("name")); err == nil {
			t.Fatal("expected missing key error")
		}
		if _, err := e.Delete("crud_users"); err == nil {
			t.Fatal("expected missing where error")
		}
		if _, err := e.SaveMany("crud_users", []crudUser{{}}, InsertColumns("name"), UpdateColumns("name")); err == nil {
			t.Fatal("expected missing conflict keys error")
		}
	})
}

func TestEngineAllowAllRowsSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		if _, err := e.InsertMany("crud_users", []crudUser{
			{Name: "All1", Email: "all1@example.com", Age: 1, Status: "active"},
			{Name: "All2", Email: "all2@example.com", Age: 2, Status: "active"},
		}, Columns("name", "email", "age", "status")); err != nil {
			t.Fatalf("InsertMany failed: %v", err)
		}

		var users []crudUser
		if err := e.SelectBy(&users, "crud_users", AllowAllRows(), OrderBy("id")); err != nil {
			t.Fatalf("SelectBy AllowAllRows failed: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("expected 2 users, got %d", len(users))
		}

		if _, err := e.Delete("crud_users", AllowAllRows()); err != nil {
			t.Fatalf("Delete AllowAllRows failed: %v", err)
		}
		users = nil
		if err := e.SelectBy(&users, "crud_users", AllowAllRows()); err != nil {
			t.Fatalf("SelectBy after delete failed: %v", err)
		}
		if len(users) != 0 {
			t.Fatalf("expected all users deleted, got %d", len(users))
		}
	})
}

func TestEngineStructuredOrderSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		if _, err := e.InsertMany("crud_users", []crudUser{
			{Name: "O1", Email: "o1@example.com", Age: 1, Status: "active"},
			{Name: "O3", Email: "o3@example.com", Age: 3, Status: "active"},
			{Name: "O2", Email: "o2@example.com", Age: 2, Status: "active"},
		}, Columns("name", "email", "age", "status")); err != nil {
			t.Fatalf("InsertMany failed: %v", err)
		}

		var users []crudUser
		if err := e.SelectBy(&users, "crud_users",
			Where("status", "active"),
			OrderDesc("age"),
			OrderAsc("email"),
		); err != nil {
			t.Fatalf("SelectBy structured order failed: %v", err)
		}
		if len(users) != 3 || users[0].Age != 3 || users[1].Age != 2 || users[2].Age != 1 {
			t.Fatalf("unexpected structured order: %#v", users)
		}

		if err := e.SelectBy(&users, "crud_users",
			Where("status", "active"),
			OrderDesc("age; DROP TABLE crud_users"),
		); err == nil {
			t.Fatal("expected invalid structured order column error")
		}
	})
}

func TestEngineCRUDMapAndPointerArgsSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		row := crudParams{
			"name":   "Map",
			"email":  "map@example.com",
			"age":    5,
			"status": "active",
		}
		if _, err := e.Insert("crud_users", row, Columns("name", "email", "age", "status")); err != nil {
			t.Fatalf("map Insert failed: %v", err)
		}

		user := &crudUser{Name: "Ptr", Email: "ptr@example.com", Age: 6, Status: "active"}
		if _, err := e.Insert("crud_users", user, Columns("name", "email", "age", "status")); err != nil {
			t.Fatalf("pointer Insert failed: %v", err)
		}

		var users []crudUser
		if err := e.SelectBy(&users, "crud_users", Where("status", "active")); err != nil {
			t.Fatalf("SelectBy failed: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("expected 2 users, got %d", len(users))
		}
	})
}

func TestEngineDynamicTypedMapSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		if _, err := e.InsertMany("crud_users", []crudUser{
			{Name: "TM1", Email: "tm1@example.com", Age: 1, Status: "active"},
			{Name: "TM2", Email: "tm2@example.com", Age: 2, Status: "inactive"},
		}, Columns("name", "email", "age", "status")); err != nil {
			t.Fatalf("InsertMany failed: %v", err)
		}

		var users []crudUser
		if err := e.Select(&users, `
			SELECT id, name, email, age, status
			FROM crud_users
			WHERE 1=1
			#[ AND status = :status ]
		`, crudParams{"status": "active"}); err != nil {
			t.Fatalf("dynamic typed map Select failed: %v", err)
		}
		if len(users) != 1 || users[0].Name != "TM1" {
			t.Fatalf("unexpected typed map result: %#v", users)
		}
	})
}

func TestEngineCRUDUnsupportedDialect(t *testing.T) {
	db := NewDb(newFakeDB(), "mysql")
	defer db.Close()
	e := NewEngine(db)

	_, err := e.Save("users", crudUser{},
		ConflictKeys("email"),
		InsertColumns("name", "email"),
		UpdateColumns("name"),
	)
	if err != ErrUnsupportedDialect {
		t.Fatalf("expected ErrUnsupportedDialect, got %v", err)
	}

	if err := e.InsertReturning(&struct{}{}, "users", crudUser{},
		Columns("name", "email"),
		Returning("id"),
	); err != ErrUnsupportedDialect {
		t.Fatalf("expected InsertReturning ErrUnsupportedDialect, got %v", err)
	}

	if err := e.InsertManyReturning(&[]int64{}, "users", []crudUser{{}},
		Columns("name", "email"),
		Returning("id"),
	); err != ErrUnsupportedDialect {
		t.Fatalf("expected InsertManyReturning ErrUnsupportedDialect, got %v", err)
	}

	if _, err := e.PrepareInsert("users",
		Columns("name", "email"),
		Returning("id"),
	); err != ErrUnsupportedDialect {
		t.Fatalf("expected PrepareInsert returning ErrUnsupportedDialect, got %v", err)
	}
}

func TestEngineCRUDSQLGenerationPostgres(t *testing.T) {
	db := NewDb(newFakeDB(), "postgres")
	defer db.Close()
	e := NewEngine(db)

	insert := e.rebind(buildInsertSQL("users", []string{"name", "email"}, 2, []string{"id"}))
	if insert != "INSERT INTO users (name, email) VALUES ($1, $2), ($3, $4) RETURNING id" {
		t.Fatalf("unexpected postgres insert SQL: %s", insert)
	}

	save := e.rebind(buildSaveSQL("users",
		[]string{"name", "email", "status"},
		[]string{"name", "status"},
		[]string{"email"},
		2,
	))
	want := "INSERT INTO users (name, email, status) VALUES ($1, $2, $3), ($4, $5, $6) ON CONFLICT (email) DO UPDATE SET name = excluded.name, status = excluded.status"
	if save != want {
		t.Fatalf("unexpected postgres save SQL:\n%s\nwant:\n%s", save, want)
	}
}

func TestEngineCRUDPostgresIntegration(t *testing.T) {
	if !TestPostgres {
		t.Skip("postgres test database is not configured")
	}

	schema := Schema{
		create: `
CREATE TABLE engine_crud_users (
	id BIGSERIAL PRIMARY KEY,
	name TEXT,
	email TEXT UNIQUE,
	age INTEGER,
	status TEXT
);
`,
		drop: `DROP TABLE IF EXISTS engine_crud_users;`,
	}

	create, drop, _ := schema.Postgres()
	MultiExec(pgdb, drop)
	MultiExec(pgdb, create)
	defer MultiExec(pgdb, drop)

	mgr := NewManager()
	if err := mgr.Add("app", pgdb); err != nil {
		t.Fatalf("Add postgres DB failed: %v", err)
	}
	app := mgr.DefaultEngine()
	if app.driverName() != "postgres" {
		t.Fatalf("expected postgres engine, got %s", app.driverName())
	}

	var id int64
	if err := app.InsertReturning(&id, "engine_crud_users", crudUser{
		Name:   "PG1",
		Email:  "pg1@example.com",
		Age:    10,
		Status: "active",
	}, Columns("name", "email", "age", "status"), Returning("id")); err != nil {
		t.Fatalf("InsertReturning postgres failed: %v", err)
	}
	if id == 0 {
		t.Fatal("expected postgres returned id")
	}

	var ids []int64
	if err := app.InsertManyReturning(&ids, "engine_crud_users", []crudUser{
		{Name: "PG2", Email: "pg2@example.com", Age: 20, Status: "active"},
		{Name: "PG3", Email: "pg3@example.com", Age: 30, Status: "inactive"},
	}, Columns("name", "email", "age", "status"), Returning("id")); err != nil {
		t.Fatalf("InsertManyReturning postgres failed: %v", err)
	}
	if len(ids) != 2 || ids[0] == 0 || ids[1] == 0 {
		t.Fatalf("unexpected postgres returned ids: %#v", ids)
	}

	if _, err := app.SaveMany("engine_crud_users", []crudUser{
		{Name: "PG2x", Email: "pg2@example.com", Age: 21, Status: "updated"},
		{Name: "PG4", Email: "pg4@example.com", Age: 40, Status: "active"},
	}, ConflictKeys("email"), InsertColumns("name", "email", "age", "status"), UpdateColumns("name", "age", "status")); err != nil {
		t.Fatalf("SaveMany postgres failed: %v", err)
	}

	var got crudUser
	if err := app.GetBy(&got, "engine_crud_users", Where("email", "pg2@example.com")); err != nil {
		t.Fatalf("GetBy postgres after SaveMany failed: %v", err)
	}
	if got.Name != "PG2x" || got.Age != 21 || got.Status != "updated" {
		t.Fatalf("postgres SaveMany did not update row: %#v", got)
	}

	err := app.WithTransaction(func(tx *Engine) error {
		_, err := tx.Insert("engine_crud_users", crudUser{
			Name:   "PGRollback",
			Email:  "pg-rollback@example.com",
			Age:    50,
			Status: "active",
		}, Columns("name", "email", "age", "status"))
		if err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("expected transaction rollback error")
	}
	if err := app.GetBy(&got, "engine_crud_users", Where("email", "pg-rollback@example.com")); err != sql.ErrNoRows {
		t.Fatalf("expected rolled back postgres row to be absent, got %v", err)
	}
}

func TestEnginePrepareNamedExecutesSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		if _, err := e.Insert("crud_users", crudUser{
			Name:   "Prepared",
			Email:  "prepared@example.com",
			Age:    40,
			Status: "active",
		}, Columns("name", "email", "age", "status")); err != nil {
			t.Fatalf("Insert failed: %v", err)
		}

		stmt, err := e.PrepareNamed(`
			SELECT id, name, email, age, status
			FROM crud_users
			WHERE email = :email
		`)
		if err != nil {
			t.Fatalf("PrepareNamed failed: %v", err)
		}
		defer stmt.Close()

		var got crudUser
		if err := stmt.Get(&got, map[string]interface{}{"email": "prepared@example.com"}); err != nil {
			t.Fatalf("prepared Get failed: %v", err)
		}
		if got.Name != "Prepared" {
			t.Fatalf("unexpected prepared result: %#v", got)
		}
	})
}

func TestEnginePrepareNamedDynamicSQLite(t *testing.T) {
	withCrudSQLite(t, func(db *DB) {
		e := NewEngine(db)
		if _, err := e.InsertMany("crud_users", []crudUser{
			{Name: "D1", Email: "d1@example.com", Age: 10, Status: "active"},
			{Name: "D2", Email: "d2@example.com", Age: 20, Status: "inactive"},
			{Name: "D3", Email: "d3@example.com", Age: 30, Status: "active"},
		}, Columns("name", "email", "age", "status")); err != nil {
			t.Fatalf("InsertMany failed: %v", err)
		}

		stmt, err := e.PrepareNamed(`
			SELECT id, name, email, age, status
			FROM crud_users
			WHERE 1=1
			#[ AND status = :status ]
			#[ AND age IN :ages ]
			ORDER BY id
		`)
		if err != nil {
			t.Fatalf("PrepareNamed dynamic failed: %v", err)
		}
		defer stmt.Close()

		var users []crudUser
		err = stmt.Select(&users, map[string]interface{}{
			"status": "active",
			"ages":   []int{10, 30},
		})
		if err != nil {
			t.Fatalf("dynamic prepared Select failed: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("expected 2 dynamic users, got %d", len(users))
		}

		users = nil
		err = stmt.Select(&users, map[string]interface{}{
			"status": "active",
			"ages":   []int{},
		})
		if err != nil {
			t.Fatalf("dynamic prepared Select with empty IN failed: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("expected empty IN block to be removed, got %d users", len(users))
		}
	})
}
