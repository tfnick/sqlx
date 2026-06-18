package sqlx

import (
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

func benchmarkCrudEngine(b *testing.B) *Engine {
	raw, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		b.Fatalf("open modernc sqlite: %v", err)
	}
	db := NewDb(raw, "sqlite")
	MultiExec(db, `
DROP TABLE IF EXISTS bench_users;
CREATE TABLE bench_users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT,
	email TEXT UNIQUE,
	age INTEGER,
	status TEXT
);
`)
	b.Cleanup(func() {
		MultiExec(db, `DROP TABLE IF EXISTS bench_users;`)
		db.Close()
	})
	return NewEngine(db)
}

func BenchmarkEngineExecPInsert(b *testing.B) {
	e := benchmarkCrudEngine(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.ExecP(
			"INSERT INTO bench_users (name, email, age, status) VALUES (?, ?, ?, ?)",
			"Tom",
			fmt.Sprintf("execp-%d@example.com", i),
			20,
			"active",
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineInsert(b *testing.B) {
	e := benchmarkCrudEngine(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Insert("bench_users", crudUser{
			Name:   "Tom",
			Email:  fmt.Sprintf("insert-%d@example.com", i),
			Age:    20,
			Status: "active",
		}, Columns("name", "email", "age", "status"))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineInsertMany100(b *testing.B) {
	e := benchmarkCrudEngine(b)
	rows := make([]crudUser, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range rows {
			rows[j] = crudUser{
				Name:   "Tom",
				Email:  fmt.Sprintf("insertmany-%d-%d@example.com", i, j),
				Age:    20,
				Status: "active",
			}
		}
		if _, err := e.InsertMany("bench_users", rows, Columns("name", "email", "age", "status")); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineExecNamedInsert(b *testing.B) {
	e := benchmarkCrudEngine(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.ExecNamed(
			"INSERT INTO bench_users (name, email, age, status) VALUES (:name, :email, :age, :status)",
			crudUser{
				Name:   "Tom",
				Email:  fmt.Sprintf("named-%d@example.com", i),
				Age:    20,
				Status: "active",
			},
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineDynamicSelect(b *testing.B) {
	e := benchmarkCrudEngine(b)
	rows := make([]crudUser, 100)
	for i := range rows {
		rows[i] = crudUser{
			Name:   "Tom",
			Email:  fmt.Sprintf("dynamic-seed-%d@example.com", i),
			Age:    i,
			Status: "active",
		}
	}
	if _, err := e.InsertMany("bench_users", rows, Columns("name", "email", "age", "status")); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var users []crudUser
		err := e.Select(&users, `
			SELECT id, name, email, age, status
			FROM bench_users
			WHERE 1=1
			#[ AND status = :status ]
			#[ AND age IN :ages ]
		`, map[string]interface{}{
			"status": "active",
			"ages":   []int{1, 2, 3, 4, 5},
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPreparedInsertExec(b *testing.B) {
	e := benchmarkCrudEngine(b)
	stmt, err := e.PrepareInsert("bench_users", Columns("name", "email", "age", "status"))
	if err != nil {
		b.Fatal(err)
	}
	defer stmt.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := stmt.Exec(crudUser{
			Name:   "Tom",
			Email:  fmt.Sprintf("prepared-%d@example.com", i),
			Age:    20,
			Status: "active",
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineSaveMany100(b *testing.B) {
	e := benchmarkCrudEngine(b)
	rows := make([]crudUser, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range rows {
			rows[j] = crudUser{
				Name:   "Tom",
				Email:  fmt.Sprintf("savemany-%d-%d@example.com", i, j),
				Age:    20,
				Status: "active",
			}
		}
		_, err := e.SaveMany("bench_users", rows,
			ConflictKeys("email"),
			InsertColumns("name", "email", "age", "status"),
			UpdateColumns("name", "age", "status"),
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPreparedSaveExecMany100(b *testing.B) {
	e := benchmarkCrudEngine(b)
	stmt, err := e.PrepareSave("bench_users",
		ConflictKeys("email"),
		InsertColumns("name", "email", "age", "status"),
		UpdateColumns("name", "age", "status"),
	)
	if err != nil {
		b.Fatal(err)
	}
	defer stmt.Close()

	rows := make([]crudUser, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range rows {
			rows[j] = crudUser{
				Name:   "Tom",
				Email:  fmt.Sprintf("prepared-save-%d-%d@example.com", i, j),
				Age:    20,
				Status: "active",
			}
		}
		if _, err := stmt.ExecMany(rows); err != nil {
			b.Fatal(err)
		}
	}
}
