# Database Guidelines

> Database patterns and conventions for this project.

---

## Overview

sqlx extends the standard `database/sql` package. It wraps `sql.DB`, `sql.Tx`, `sql.Stmt`, `sql.Conn`, `sql.Row`, and `sql.Rows` with enhanced types that add struct scanning, named parameters, and automatic bind variable rebinding.

The project does **not** implement its own ORM or migration system — it is a lightweight query library. Database drivers are imported by consumers, not by this library directly (test dependencies only).

---

## Query Patterns

### Core API: `Select` and `Get`

```go
// Select scans all rows into a slice
var people []Person
err := db.Select(&people, "SELECT * FROM people ORDER BY first_name ASC")

// Get scans a single row into a struct
var person Person
err := db.Get(&person, "SELECT * FROM people WHERE id = ?", id)
```

### Named Parameters

Named parameters use `:name` syntax and accept structs or maps:

```go
// With a struct (uses `db` tags)
type Person struct {
    FirstName string `db:"first_name"`
}
rows, err := db.NamedQuery(`SELECT * FROM people WHERE first_name = :first_name`, p)

// With a map
rows, err := db.NamedQuery(`SELECT * FROM people WHERE first_name = :fn`, map[string]interface{}{"fn": "John"})
```

### Bind Variable Handling

Automatic rebinding of `?` to the driver-appropriate placeholder:

| Bind Type | Driver Example | Placeholder |
|-----------|---------------|-------------|
| `QUESTION` | MySQL, SQLite | `?` |
| `DOLLAR` | PostgreSQL, CockroachDB | `$1, $2` |
| `NAMED` | Oracle | `:name` |
| `AT` | SQL Server, Azure SQL | `@p1, @p2` |

Defined in `bind.go:24-29`. Drivers are registered at init time via `BindDriver()`.

### IN Clause Expansion

Use `In()` to expand slice arguments for `IN` clauses:

```go
query, args, err := sqlx.In("SELECT * FROM people WHERE id IN (?)", ids)
query = db.Rebind(query) // rebind ? to driver-specific placeholder
db.Select(&people, query, args...)
```

### Dynamic SQL Engine

The `Engine` type (`engine.go`) supports conditional SQL blocks using `#[ ]` syntax:

```go
sql := `SELECT id, name FROM user WHERE 1=1
#[ AND name LIKE :name ]
#[ AND age >= :min_age ]`

params := map[string]interface{}{"name": "%tom%"}
err := engine.Select(ctx, &users, sql, params)
// Blocks whose named params are nil/empty are removed from the query.
```

### Standard Library Handle Integration

Some external database-backed libraries require standard `database/sql` handles
instead of sqlx wrappers. Keep repository code Engine-first, but use explicit
bridge methods at application wiring boundaries:

```go
rawDB := app.StdDB()
```

For libraries that must participate in the same transaction as Engine-managed
repository work, use `WithTransactionRaw`:

```go
err := app.WithTransactionRaw(ctx, nil, func(tx *sqlx.Engine, rawTx *sql.Tx) error {
    if err := repoUsing(tx).Write(ctx, row); err != nil {
        return err
    }
    return enqueueWithStdTx(ctx, rawTx)
})
```

Rules:

1. Prefer `*Engine` in repositories and services.
2. Use `StdDB`, `StdTx`, `Manager.GetDB`, `Manager.MustDB`, or `Manager.StdDB`
   only at integration boundaries.
3. Do not add external integration dependencies to the root package when a
   standard `database/sql` bridge is sufficient.
4. Transaction-bound Engines do not expose a standard DB; use the raw
   transaction passed by `WithTransactionRaw`.

#### Standard Handle Bridge Contract

1. Scope / Trigger

Use this contract when integrating an external SQL-backed library such as a
queue, cache, migration helper, or advisory-lock helper that requires
`*sql.DB` or `*sql.Tx`.

2. Signatures

```go
func (db *DB) StdDB() *sql.DB
func (tx *Tx) StdTx() *sql.Tx
func (m *Manager) GetDB(name string) (*DB, error)
func (m *Manager) MustDB(name string) *DB
func (m *Manager) StdDB(name string) (*sql.DB, error)
func (e *Engine) StdDB() *sql.DB
func (e *Engine) WithTransactionRaw(ctx context.Context, opts *sql.TxOptions, fn func(*Engine, *sql.Tx) error) error
```

3. Contracts

* Empty manager names resolve to `"app"`.
* `GetDB` and `StdDB` return the same missing-database error style as
  `GetEngine`: `sqlx: database "<name>" is not registered`.
* `MustDB` panics only as a convenience wrapper around `GetDB`.
* `Engine.StdDB` returns nil for transaction-bound Engines.
* `WithTransactionRaw` rejects nested Engine transactions with
  `sqlx: nested Engine transactions are not supported`.

4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Registered manager DB requested | Returns registered `*DB` or `*sql.DB` |
| Missing manager DB requested | Returns an error |
| `MustDB` missing manager DB | Panics |
| `WithTransactionRaw` callback returns error | Rolls back and returns callback error |
| `WithTransactionRaw` callback panics | Rolls back and re-panics |
| `WithTransactionRaw` commit fails | Returns commit error |
| Nested `WithTransactionRaw` on transaction Engine | Returns nested transaction error |

5. Good/Base/Bad Cases

Good:

```go
queue := newQueue(app.StdDB())
```

Base:

```go
raw, err := manager.StdDB("app")
```

Bad:

```go
// Do not make repositories depend on raw database handles by default.
type Repo struct {
    db *sql.DB
}
```

6. Tests Required

* DB/Tx accessors return the embedded standard handles.
* Manager accessors respect default name resolution and missing-name errors.
* `WithTransactionRaw` passes non-nil Engine and standard transaction handles.
* `WithTransactionRaw` returns callback errors and rejects nested Engine
  transactions.

7. Wrong vs Correct

Wrong:

```go
// Bypasses the Engine-first repository convention for normal application code.
repo := NewRepo(engine.DB().DB)
```

Correct:

```go
// Keep repositories Engine-first and use raw handles only for external library boundaries.
repo := NewRepo(engine)
queue := newQueue(engine.StdDB())
```

---

## Naming Conventions

- **Struct fields → columns**: Use `db` struct tag. Without a tag, the field name is lowercased.
  ```go
  type Person struct {
      FirstName string `db:"first_name"` // maps to "first_name" column
      Email     string                   // maps to "email" column (lowercased)
  }
  ```
- **Table/column names in queries**: SQL standard. No project-level naming enforced — this is a library, not an application.

---

## Struct Scanning Rules

1. Fields are matched to columns by `db` struct tag, falling back to the global `NameMapper` function (default: `strings.ToLower`)
2. `NameMapper` must be set **before** any struct scanning — mappings are cached on first use
3. Missing columns cause an error unless `.Unsafe()` is used
4. Embedded structs are traversed recursively
5. Types implementing `sql.Scanner` are scanned directly (not decomposed by fields)

---

## Common Mistakes

- **Setting `NameMapper` after scanning**: Mappings are cached, so late changes are ignored. Always set `NameMapper` before any struct scan.
- **Forgetting `Rebind` with `In()`**: `In()` returns `?` placeholders. Call `db.Rebind()` before executing if using a non-QUESTION driver.
- **Empty slices in `In()`**: Passing an empty slice to `In()` returns an error ("empty slice passed to 'in' query").
