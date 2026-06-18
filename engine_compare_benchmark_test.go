package sqlx

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

const compareSeedRows = 10000
const compareBatchRows = 1000
const compareQueryLimit = 100

type compareUser struct {
	ID     int64  `db:"id"`
	Name   string `db:"name"`
	Email  string `db:"email"`
	Age    int    `db:"age"`
	Status string `db:"status"`
}

func benchmarkCompareSQLite(b *testing.B) *Engine {
	dsn := filepath.Join(b.TempDir(), "engine_compare.sqlite") + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		b.Fatalf("open modernc sqlite: %v", err)
	}
	db := NewDb(raw, "sqlite")
	MultiExec(db, `
DROP TABLE IF EXISTS engine_compare_users;
CREATE TABLE engine_compare_users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT,
	email TEXT UNIQUE,
	age INTEGER,
	status TEXT
);
CREATE INDEX idx_engine_compare_users_status ON engine_compare_users(status);
`)
	b.Cleanup(func() {
		MultiExec(db, `DROP TABLE IF EXISTS engine_compare_users;`)
		db.Close()
	})
	return NewEngine(db)
}

func benchmarkComparePostgres(b *testing.B) *Engine {
	if !TestPostgres {
		b.Skip("postgres test database is not configured")
	}
	MultiExec(pgdb, `
DROP TABLE IF EXISTS engine_compare_users;
CREATE TABLE engine_compare_users (
	id BIGSERIAL PRIMARY KEY,
	name TEXT,
	email TEXT UNIQUE,
	age INTEGER,
	status TEXT
);
CREATE INDEX idx_engine_compare_users_status ON engine_compare_users(status);
`)
	b.Cleanup(func() {
		MultiExec(pgdb, `DROP TABLE IF EXISTS engine_compare_users;`)
	})
	return NewEngine(pgdb)
}

func compareRows(prefix string, batch int, seq int) []compareUser {
	rows := make([]compareUser, batch)
	base := seq * batch
	for i := range rows {
		n := base + i
		status := "active"
		if n%2 == 0 {
			status = "inactive"
		}
		rows[i] = compareUser{
			Name:   fmt.Sprintf("%s-%d", prefix, n),
			Email:  fmt.Sprintf("%s-%d@example.com", prefix, n),
			Age:    n % 100,
			Status: status,
		}
	}
	return rows
}

func seedCompareRows(b *testing.B, e *Engine, prefix string, total int) {
	for start := 0; start < total; start += compareBatchRows {
		batch := compareBatchRows
		if start+batch > total {
			batch = total - start
		}
		if _, err := e.InsertMany("engine_compare_users", compareRows(prefix, batch, start/compareBatchRows),
			Columns("name", "email", "age", "status"),
		); err != nil {
			b.Fatalf("seed InsertMany failed: %v", err)
		}
	}
}

func benchmarkCompareWrite(b *testing.B, e *Engine, prefix string) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.InsertMany("engine_compare_users", compareRows(prefix, compareBatchRows, i),
			Columns("name", "email", "age", "status"),
		); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N*compareBatchRows)/b.Elapsed().Seconds(), "rows/s")
}

func benchmarkCompareQuery(b *testing.B, e *Engine) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var users []compareUser
		if err := e.SelectBy(&users, "engine_compare_users",
			Where("status", "active"),
			Columns("id", "name", "email", "age", "status"),
			OrderDesc("id"),
			LimitOffset(compareQueryLimit, 0),
		); err != nil {
			b.Fatal(err)
		}
		if len(users) != compareQueryLimit {
			b.Fatalf("expected %d users, got %d", compareQueryLimit, len(users))
		}
	}
	elapsed := b.Elapsed().Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "queries/s")
	b.ReportMetric(float64(b.N*compareQueryLimit)/elapsed, "rows/s")
}

func BenchmarkEngineCompareWriteSQLite(b *testing.B) {
	benchmarkCompareWrite(b, benchmarkCompareSQLite(b), "sqlite-write")
}

func BenchmarkEngineCompareWritePostgres(b *testing.B) {
	benchmarkCompareWrite(b, benchmarkComparePostgres(b), "pg-write")
}

func BenchmarkEngineCompareQuerySQLite(b *testing.B) {
	e := benchmarkCompareSQLite(b)
	seedCompareRows(b, e, "sqlite-query", compareSeedRows)
	benchmarkCompareQuery(b, e)
}

func BenchmarkEngineCompareQueryPostgres(b *testing.B) {
	e := benchmarkComparePostgres(b)
	seedCompareRows(b, e, "pg-query", compareSeedRows)
	benchmarkCompareQuery(b, e)
}
