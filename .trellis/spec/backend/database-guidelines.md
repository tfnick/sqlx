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
