# Quality Guidelines

> Code quality standards for this project.

---

## Overview

sqlx follows standard Go conventions backed by automated tooling. Quality is enforced via `make lint` which runs `go vet` and `staticcheck`.

---

## Required Patterns

### Wrapper pattern

All sqlx types embed the corresponding `database/sql` type:

```go
type DB struct {
    *sql.DB
    driverName string
    unsafe     bool
    Mapper     *reflectx.Mapper
}

type Tx struct {
    *sql.Tx
    driverName string
    unsafe     bool
    Mapper     *reflectx.Mapper
}
```

All wrapper types must include: the embedded sql type, `driverName`, `unsafe`, and `Mapper`.

### Interface composition

Core interfaces use composition to build up capabilities:

```go
type Ext interface {
    binder
    Queryer
    Execer
}
```

### Reflection optimization

Reflection results are cached in `reflectx.Mapper`. Do not bypass the cache — always use `mapper()` or the type's `Mapper` field.

### Struct tag convention

Use `"db"` as the struct tag key (not `"json"` or custom keys):

```go
type Person struct {
    Name string `db:"name"`
}
```

### Method naming

- `*x` suffix for sqlx-specific methods (`Queryx`, `QueryRowx`, `Beginx`, `Preparex`, `Select`)
- `Must*` prefix for panicking variants (`MustExec`, `MustBegin`, `MustOpen`)
- `*Named` for named-parameter variants (`NamedQuery`, `NamedExec`, `PrepareNamed`)
- `*Context` suffix for context-aware variants (`SelectContext`, `GetContext`)

---

## Forbidden Patterns

1. **No third-party dependencies beyond database drivers** (test-only). The library's only dependencies in `go.mod` are driver imports used in tests.
2. **No stateful globals without synchronization**. Package-level mutable state (e.g., `mpr`, `binds`) must use `sync.Mutex` or `sync.Map`.
3. **No reflection without caching**. `reflectx.Mapper` caches struct metadata — raw `reflect` calls in hot paths are unacceptable.
4. **No panics except in `Must*` functions**. Library code must return errors, not panic.
5. **No breaking public API changes** without a major version bump.

---

## Testing Requirements

### Test infrastructure

- Tests use real databases (PostgreSQL, MySQL, SQLite), not mocks
- Drivers imported in test files only
- Run with race detector: `go test -v -race -count=1 ./...`

### Test conventions

```bash
# Run all tests
go test -v -count=1 ./...

# Run with race detection
go test -v -race -count=1 ./...

# Run specific package
go test -v -count=1 ./reflectx

# Run specific test
go test -v -run TestFunctionName ./...
```

### What to test

- All new public API functions must have corresponding tests co-located (`<file>_test.go`)
- Test both success and error paths
- Test across supported bind variable types (QUESTION, DOLLAR, NAMED, AT)
- Test edge cases: empty slices, nil inputs, missing columns, zero-value structs

---

## Linting

```bash
make lint   # runs: go vet ./... && staticcheck -checks=all ./...
make fmt    # runs: goimports -local github.com/tfnick/sqlx -w
```

Before committing, ensure `make lint` passes cleanly.

---

## Code Review Checklist

- [ ] New public API follows the `*x` / `Must*` / `*Named` / `*Context` naming convention
- [ ] All exported functions have Go doc comments
- [ ] New wrapper types include `driverName`, `unsafe`, `Mapper` fields
- [ ] Reflection hot paths use the cached mapper, not raw `reflect`
- [ ] Error returns (not panics) for library code
- [ ] Tests cover both success and failure paths
- [ ] `make lint` passes
- [ ] `go test -race -count=1 ./...` passes
- [ ] No new dependencies added without strong justification
