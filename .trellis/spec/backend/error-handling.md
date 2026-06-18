# Error Handling

> How errors are handled in this project.

---

## Overview

sqlx follows standard Go error handling conventions. Errors are returned as the last return value. There are no custom error types defined — the project uses `errors.New()` and standard library errors.

---

## Error Types

The project does **not** define custom error types. Errors come from:

- **`errors.New("message")`** — for library-level errors (e.g., `bind.go:180`, `bind.go:240`)
- **`sql.ErrNoRows`** — from the standard library, returned when `Get` finds no results
- **Driver errors** — propagated directly from the underlying `database/sql` driver

---

## Error Handling Patterns

### Pattern 1: Deferred error in Row

`Row` stores an error that is checked before any operation:

```go
// sqlx.go:177-179
func (r *Row) Scan(dest ...interface{}) error {
    if r.err != nil {
        return r.err
    }
    // ... proceed with scan
}
```

### Pattern 2: Error check with early return

Standard Go pattern used throughout:

```go
// sqlx.go:264-269
func Open(driverName, dataSourceName string) (*DB, error) {
    db, err := sql.Open(driverName, dataSourceName)
    if err != nil {
        return nil, err
    }
    return &DB{DB: db, driverName: driverName, Mapper: mapper()}, err
}
```

### Pattern 3: Must* functions (panic on error)

For convenience in application code where errors are unexpected:

```go
// sqlx.go:272-279
func MustOpen(driverName, dataSourceName string) *DB {
    db, err := Open(driverName, dataSourceName)
    if err != nil {
        panic(err)
    }
    return db
}
```

### Pattern 4: Interface compliance errors

Errors for type mismatches use `errors.New()` with capitalized messages:

```go
// types/types.go:39
return errors.New("Incompatible type for GzippedText")

// types/types.go:106
return errors.New("Incompatible type for JSONText")
```

### Pattern 5: Sentinel zero-value on error

When returning a value type that can't be nil, return the zero value:

```go
// named.go:45
return *new(sql.Result), err
```

---

## Error Propagation Rules

1. **Library errors**: Return errors as-is. Do not wrap (consistent with Go 1.10-era style).
2. **No error wrapping**: This project predates `fmt.Errorf("%w")`. Errors are returned directly.
3. **No panic in library code**: Panics are only in `Must*` convenience functions, never in the core API.
4. **Resource cleanup on error**: `Rows` are always closed via `defer` or in the error path.

---

## Common Mistakes

- **Not checking `Row.Err()`**: `Row.Scan()` returns the deferred error, but calling `Err()` explicitly can catch errors before Scan.
- **Ignoring `Rows.Err()`**: After iterating rows, always check `rows.Err()` — iteration errors are separate from scan errors.
