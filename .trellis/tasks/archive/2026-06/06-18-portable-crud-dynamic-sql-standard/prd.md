# Unified Engine API for Portable SQL, CRUD Calls, and Transactions

## Background

Current sqlx already has the main portability primitives: bind rebinding, named parameter binding, `IN` slice expansion, and dynamic SQL preprocessing. The missing client-facing design is a single entrypoint that works for one database, many databases, normal execution, transactions, CRUD-style SQL calls, and dynamic SQL.

The new standard should reduce the user's mental model to one sentence:

```text
Always get an Engine first, then call everything from that Engine.
```

## Goal

Design a unified Engine-first API for:

- Multi-database access through `db.GetEngine("dbName")`.
- Simple positional SQL.
- Named parameter SQL.
- Dynamic optional SQL conditions.
- `IN` slice parameters.
- Simple CRUD-style calls through Engine methods.
- Transactions.
- Context-aware execution.
- Prepared statements and cached metadata for hot paths.
- Low-friction migration from SQLite to PostgreSQL when SQL dialect differences are out of scope.

## Non-Goals

- Do not translate SQL dialects between SQLite and PostgreSQL.
- Do not translate arbitrary SQL written by the application. Engine CRUD helpers may generate dialect-specific SQL for supported single-table operations.
- Do not abstract vendor-specific DDL.
- Do not build a full ORM.
- Do not introduce a `Table(...).Model(...)` CRUD DSL as the primary client-facing API.
- Do not infer joins or nested object graphs.
- Do not hide transaction scope; starting a transaction should still be explicit.
- Do not force users to choose between `*DB`, `*Tx`, `Runner`, and `Engine` at call sites.

## Core Design

The top-level database holder, currently represented by `Manager`, should expose Engine lookup methods:

```go
engine, err := db.GetEngine("app")
engine := db.MustEngine("app")
engine := db.Engine("app")
engine := db.DefaultEngine()
```

Recommended semantics:

- `GetEngine(name)` returns `(*Engine, error)`.
- `MustEngine(name)` panics on missing database and is useful during initialization.
- `Engine(name)` is kept as a short panic-on-missing alias.
- `DefaultEngine()` returns the default database Engine and is useful for single-database applications.
- `name == ""` maps to the default database, usually `"app"`.
- Each database name returns a stable, cached Engine bound to that database.

For a single database application, the same pattern still applies:

```go
db := sqlx.NewManager()
db.MustOpen("app", "sqlite3", dsn)
app := db.DefaultEngine()
```

For a multi-database application:

```go
app := db.MustEngine("app")
logs := db.MustEngine("logs")
tenant := db.MustEngine("tenant_001")
```

## Engine Responsibilities

`*Engine` becomes the single client-facing execution surface:

```go
type Engine struct {
    // bound to either a DB connection pool or a transaction
}
```

It should support:

- `ExecP`, `GetP`, `SelectP` for portable positional SQL using `?`.
- `ExecNamed`, `GetNamed`, `SelectNamed` for portable named SQL using `:name`.
- `Exec`, `Get`, `Select` for dynamic SQL with `#[ ... ]`.
- Single-table CRUD helpers: `Insert`, `InsertReturning`, `Update`, `Delete`, `GetBy`, and `SelectBy`.
- Batch CRUD helpers: `InsertMany`, `InsertManyReturning`, `Save`, and `SaveMany`.
- Prepared CRUD plans: `PrepareInsert` and `PrepareSave` for hot batch paths.
- `WithTransaction` for transaction callbacks that receive a transaction-bound Engine.
- Context-aware variants.
- Prepared dynamic statements for hot paths.

This means repository code depends on `*sqlx.Engine`, not `*sqlx.DB`, `*sqlx.Tx`, or `Runner`.

## API Contract

The implementation should use `demo.md` as the source of truth for the client-facing calling style. The public API should stay explicit and small: Engine methods plus option helpers, not a chainable table/model DSL.

### Manager API

`Manager` owns named databases and cached Engines:

```go
func (m *Manager) GetEngine(name string) (*Engine, error)
func (m *Manager) MustEngine(name string) *Engine
func (m *Manager) Engine(name string) *Engine
func (m *Manager) DefaultEngine() *Engine
```

Semantics:

- `name == ""` maps to the default database name, initially `"app"`.
- Repeated calls for the same database return the same cached `*Engine`.
- Missing database returns an error from `GetEngine`, panics from `MustEngine`, `Engine`, and `DefaultEngine`.
- Engine lookup must be safe for concurrent callers.
- Closing the manager still closes all underlying databases; cached Engines must not own separate connections.

### Engine Base Query API

Engine must expose the same execution surface for normal DB-backed and transaction-backed Engines:

```go
func (e *Engine) ExecP(query string, args ...interface{}) (sql.Result, error)
func (e *Engine) GetP(dest interface{}, query string, args ...interface{}) error
func (e *Engine) SelectP(dest interface{}, query string, args ...interface{}) error

func (e *Engine) ExecNamed(query string, arg interface{}) (sql.Result, error)
func (e *Engine) GetNamed(dest interface{}, query string, arg interface{}) error
func (e *Engine) SelectNamed(dest interface{}, query string, arg interface{}) error

func (e *Engine) Exec(query string, arg ...interface{}) (sql.Result, error)
func (e *Engine) Get(dest interface{}, query string, arg ...interface{}) error
func (e *Engine) Select(dest interface{}, query string, arg ...interface{}) error
```

Context variants should be available for every execution method:

```go
func (e *Engine) ExecPContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
func (e *Engine) GetPContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
func (e *Engine) SelectPContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
```

The same pattern applies to `ExecNamedContext`, `GetNamedContext`, `SelectNamedContext`, dynamic `ExecContext`, `GetContext`, `SelectContext`, and CRUD helper context variants.

### Dynamic SQL Contract

Dynamic SQL follows the pipeline used in `demo.md`:

```text
Preprocess #[ ... ] blocks -> bind named params -> expand IN slices -> rebind placeholders -> execute
```

Rules:

- A dynamic block is kept only when at least one named parameter inside the block has a meaningful value.
- Empty slices remove the optional block instead of calling `In()` with an empty slice.
- `IN :ids` is the recommended client-facing syntax for slice params.
- Dynamic SQL accepts structs using `db` tags and `map[string]interface{}` or compatible map types.
- The same dynamic SQL methods must work on transaction-bound Engines.

### CRUD Option API

CRUD helpers use plain option functions. Option names should match `demo.md`:

```go
sqlx.Columns("name", "email", "age", "status")
sqlx.InsertColumns("name", "email", "age", "status")
sqlx.UpdateColumns("name", "age", "status")
sqlx.Keys("id")
sqlx.ConflictKeys("email")
sqlx.Returning("id")
sqlx.Where("status", "active")
sqlx.OrderDesc("id")
sqlx.OrderAsc("email")
sqlx.OrderBy("id DESC")
sqlx.LimitOffset(limit, offset)
sqlx.AllowAllRows()
sqlx.BatchSize(500)
```

Validation rules:

- Table and column names are identifiers, not bind values. They must be validated before generating SQL.
- Empty table name, empty required column lists, empty conflict keys for `Save`/`SaveMany`, and invalid identifiers return errors.
- `Where` values are always bound parameters.
- `OrderAsc` and `OrderDesc` validate column identifiers and are the recommended ordering helpers.
- `OrderBy` is retained as a raw SQL escape hatch for trusted/static application SQL.
- `AllowAllRows` is required for `Delete`, `GetBy`, or `SelectBy` without any `Where` options.
- `BatchSize` overrides the automatic batch chunk size for `InsertMany`, `InsertManyReturning`, and `SaveMany`.
- CRUD helpers bind values from structs using `db` tags and from maps by key.
- Missing struct fields or map keys return errors.

### Engine CRUD API

Single-row helpers:

```go
func (e *Engine) Insert(table string, arg interface{}, opts ...CrudOption) (sql.Result, error)
func (e *Engine) InsertReturning(dest interface{}, table string, arg interface{}, opts ...CrudOption) error
func (e *Engine) Update(table string, arg interface{}, opts ...CrudOption) (sql.Result, error)
func (e *Engine) Delete(table string, opts ...CrudOption) (sql.Result, error)
func (e *Engine) GetBy(dest interface{}, table string, opts ...CrudOption) error
func (e *Engine) SelectBy(dest interface{}, table string, opts ...CrudOption) error
func (e *Engine) Save(table string, arg interface{}, opts ...CrudOption) (sql.Result, error)
```

Batch helpers:

```go
func (e *Engine) InsertMany(table string, args interface{}, opts ...CrudOption) (sql.Result, error)
func (e *Engine) InsertManyReturning(dest interface{}, table string, args interface{}, opts ...CrudOption) error
func (e *Engine) SaveMany(table string, args interface{}, opts ...CrudOption) (sql.Result, error)
```

Context variants should exist for each method with the standard `Context` suffix.

Required behavior:

- `Insert` generates `INSERT INTO table (columns...) VALUES (...)`.
- `InsertReturning` hides SQLite/PostgreSQL differences where feasible. PostgreSQL may use `RETURNING`; SQLite may use `LastInsertId` for a single integer returning column.
- `Update` requires at least one key and at least one update column.
- `Delete`, `GetBy`, and `SelectBy` require at least one `Where` unless `AllowAllRows()` is provided.
- `Save` and `SaveMany` generate upsert SQL for SQLite and PostgreSQL using `ON CONFLICT (...) DO UPDATE`.
- Unsupported dialect/helper combinations return a package-level sentinel error such as `ErrUnsupportedDialect`.
- Nil args, non-struct/non-map row args, non-slice batch args, and empty batches return errors.
- Batch helpers split large batches by parameter count. The default maximum is conservative for SQLite and PostgreSQL, and callers may override rows per chunk with `BatchSize`.

### Prepared CRUD API

Prepared CRUD plans are for hot paths and batch loops:

```go
func (e *Engine) PrepareInsert(table string, opts ...CrudOption) (*CrudStmt, error)
func (e *Engine) PrepareSave(table string, opts ...CrudOption) (*CrudStmt, error)

type CrudStmt struct {
    // internal prepared statement and cached bind/column metadata
}

func (s *CrudStmt) Exec(arg interface{}) (sql.Result, error)
func (s *CrudStmt) ExecMany(args interface{}) (sql.Result, error)
func (s *CrudStmt) ExecReturning(dest interface{}, arg interface{}) error
func (s *CrudStmt) Close() error
```

The prepared plan should cache:

- Validated table and column names.
- Driver dialect and bind type.
- Generated SQL template.
- Named parameter order.
- Struct field traversals through the existing `reflectx.Mapper`.

### Transaction API

Transactions start from Engine:

```go
func (e *Engine) WithTransaction(fn func(*Engine) error) error
func (e *Engine) WithTransactionContext(ctx context.Context, opts *sql.TxOptions, fn func(*Engine) error) error
```

Transaction behavior:

- Callback receives a transaction-bound `*Engine`.
- The transaction Engine exposes the same methods as the DB-backed Engine.
- Commit when callback returns nil.
- Roll back when callback returns error.
- Roll back before re-panicking when callback panics.
- Do not start nested transactions automatically. A transaction-bound Engine calling `WithTransaction` should return an error unless explicit savepoint support is added later.

## API Ergonomics

The client should not need to remember whether a method belongs to `DB`, `Tx`, `Manager`, or a helper package. The normal flow is:

```go
app := db.MustEngine("app")
repo := NewUserRepo(app)
```

Inside repositories:

```go
err := r.e.Select(&users, searchSQL, query)
```

Inside transactions:

```go
return r.e.WithTransaction(func(tx *sqlx.Engine) error {
    repo := NewUserRepo(tx)
    return repo.Update(...)
})
```

The same repository method should work with a normal Engine and a transaction-bound Engine.

## Canonical SQL Rules

Migration-friendly application SQL must follow these rules:

- Use `:name` for named parameters.
- Use `#[ ... ]` for optional dynamic SQL blocks.
- Use `IN :ids` for slice parameters handled by the Engine.
- Use `?` only with `ExecP`, `GetP`, or `SelectP`.
- Use Engine CRUD helpers for simple single-table insert, update, delete, read-by-condition, batch insert, and batch save/upsert.
- Avoid direct `$1`, `$2`, etc. in application SQL.
- Avoid manual string concatenation for optional SQL fragments.
- Keep unavoidable database-specific SQL in explicitly named driver-specific methods.

## Portable Positional SQL

Simple positional SQL stays short:

```go
user, err := app.GetP(&user, "SELECT * FROM users WHERE id = ?", id)
```

The Engine calls `Rebind` internally.

## Named SQL

Named SQL is the default for most repository code:

```go
err := app.GetNamed(&user, `
    SELECT id, name, email
    FROM users
    WHERE email = :email
`, map[string]any{
    "email": email,
})
```

The Engine handles named binding and driver-specific bind conversion.

## Dynamic SQL

Dynamic SQL should be one call from Engine:

```go
err := app.Select(&users, `
    SELECT id, name, email
    FROM users
    WHERE 1=1
    #[ AND status = :status ]
    #[ AND name LIKE :name ]
    #[ AND id IN :ids ]
`, query)
```

The internal pipeline remains:

```text
Preprocess dynamic blocks -> bind named params -> expand slices -> rebind placeholders -> execute
```

## Engine CRUD API

Simple single-table CRUD should be expressed through Engine methods when the operation fits the helper model. The client should not need to learn a separate `Table(...).Model(...)` DSL, and should not need to hand-write vendor-specific insert/update/upsert SQL for common cases.

```go
_, err := app.Insert("users", user,
    sqlx.Columns("name", "email", "status"),
)

var id int64
err := app.InsertReturning(&id, "users", user,
    sqlx.Columns("name", "email", "status"),
    sqlx.Returning("id"),
)

_, err = app.Update("users", user,
    sqlx.Keys("id"),
    sqlx.Columns("name", "email", "status"),
)

_, err = app.Delete("users", sqlx.Where("id", id))
```

Read helpers cover simple single-table reads while explicit SQL remains available for joins, aggregates, and vendor-specific logic:

```go
err := app.GetBy(&user, "users",
    sqlx.Where("id", id),
    sqlx.Columns("id", "name", "email", "status"),
)

err = app.SelectBy(&users, "users",
    sqlx.Where("status", "active"),
    sqlx.OrderDesc("id"),
    sqlx.LimitOffset(limit, offset),
)
```

Batch insert and batch save/upsert are first-class Engine operations:

```go
_, err := app.InsertMany("users", users,
    sqlx.Columns("name", "email", "status"),
)

_, err = app.SaveMany("users", users,
    sqlx.ConflictKeys("email"),
    sqlx.InsertColumns("name", "email", "status"),
    sqlx.UpdateColumns("name", "status"),
)
```

SQLite and PostgreSQL should be supported for the same CRUD helper calls. Unsupported dialect/helper combinations should return a clear `ErrUnsupportedDialect` instead of silently falling back to unsafe or partial behavior. Optional code generation may be considered later, but it should not replace the unified Engine method surface.

## Transactions

Transactions should also start from Engine:

```go
err := app.WithTransaction(func(tx *sqlx.Engine) error {
    repo := NewUserRepo(tx)
    return repo.Update(...)
})
```

Context and transaction options:

```go
err := app.WithTransactionContext(ctx, &sql.TxOptions{
    Isolation: sql.LevelSerializable,
}, func(tx *sqlx.Engine) error {
    repo := NewUserRepo(tx)
    return repo.Update(...)
})
```

The transaction-bound Engine should expose the same methods as a normal Engine. This is the key to keeping repository code simple.

Transaction behavior:

- Commit when callback returns nil.
- Roll back when callback returns error.
- Roll back before re-panicking when callback panics.
- Preserve context cancellation behavior.
- Avoid reparsing SQL just because execution is inside a transaction.

## Multi-Database Support

The database holder should support multiple named databases and multiple named Engines:

```go
db.MustOpen("app", "sqlite3", appDSN)
db.MustOpen("logs", "sqlite3", logsDSN)

app := db.MustEngine("app")
logs := db.MustEngine("logs")
```

Cross-database workflows should make database selection explicit:

```go
userRepo := NewUserRepo(db.MustEngine("app"))
logRepo := NewAccessLogRepo(db.MustEngine("logs"))
```

The design does not attempt cross-database transactions unless the underlying database and driver explicitly support them.

## Performance Model

The convenience API should have a clear performance model:

- `GetEngine(name)` and `DefaultEngine()` should be cheap lookups returning cached Engines.
- `Engine` should cache bind type and database metadata.
- Dynamic SQL templates should be cacheable.
- Prepared named and dynamic statements should cache parsed SQL and parameter metadata.
- Prepared dynamic statements should be available for hot paths.
- CRUD helpers should cache reflected column metadata and generated SQL plans.
- Batch insert and batch save should avoid per-row SQL parsing and repeated reflection.
- Transaction-bound Engine should not add extra reflection or parsing beyond the selected query API.

Benchmarks should compare:

- Raw sqlx calls versus `ExecP`, `GetP`, `SelectP`.
- Named calls with and without prepared/cached templates.
- Dynamic SQL one-shot calls versus prepared dynamic statements.
- Prepared statement cold start versus warm cache.
- `InsertMany` and `SaveMany` behavior for small and large slices.
- Prepared CRUD plan cold start versus warm cache.

## Implementation Plan

### Phase 1: Normalize Engine Around a Runner

- Refactor `Engine` so it can be bound to either `*DB` or `*Tx` without changing the public call surface.
- Add internal interfaces for the common operations needed by `Exec`, `Get`, `Select`, `PrepareNamed`, `Rebind`, `DriverName`, and mapper access.
- Keep existing dynamic SQL behavior working while this internal shape changes.
- Add tests that a DB-backed Engine and a transaction-bound Engine both execute `ExecP`, `GetP`, `SelectP`, named SQL, and dynamic SQL.

### Phase 2: Manager Engine Lookup

- Add cached Engine storage to `Manager`.
- Implement `GetEngine`, `MustEngine`, `Engine`, and `DefaultEngine`.
- Define the default database name behavior for `name == ""`.
- Ensure cache invalidation or closure behavior is documented when a database is closed/reopened.
- Test missing database, default name, repeated lookup identity, multi-database lookup, and concurrent lookup.

### Phase 3: Context and Base Engine Methods

- Add `ExecP`, `GetP`, `SelectP` and context variants.
- Add `ExecNamed`, `GetNamed`, `SelectNamed` and context variants.
- Add dynamic `ExecContext`, `GetContext`, `SelectContext`.
- Ensure all non-context methods delegate to context methods with `context.Background()`.
- Test placeholder rebinding across at least SQLite/QUESTION and PostgreSQL/DOLLAR bind modes where feasible.

### Phase 4: Dynamic SQL Hardening

- Align dynamic SQL behavior with `demo.md`.
- Ensure optional blocks with empty slices are removed before `In()` expansion.
- Support structs, pointers to structs, maps, and compatible named map types.
- Preserve existing `db` tag mapping and `NameMapper` behavior.
- Add tests for nil values, zero values, non-nil pointers to zero values, empty slices, non-empty slices, multiple slice conditions, missing params, and transaction execution.

### Phase 5: CRUD Option and Dialect Infrastructure

- Add the `CrudOption` type and option helpers from `demo.md`.
- Add validation for table and column identifiers.
- Add a small internal dialect layer for CRUD SQL generation, initially covering SQLite and PostgreSQL.
- Add `ErrUnsupportedDialect` and validation errors using the project's existing simple error style.
- Add tests for option parsing, invalid identifiers, missing columns, missing keys, empty table names, unsupported dialects, and generated SQL shape.

### Phase 6: Single-Row CRUD Helpers

- Implement `Insert`, `InsertReturning`, `Update`, `Delete`, `GetBy`, `SelectBy`, and `Save`.
- Implement context variants for each helper.
- Use existing mapper caches to bind struct fields by `db` tag.
- Keep map binding behavior consistent with named queries.
- Test struct args, pointer args, map args, missing fields, zero values, returning behavior, no-row behavior, and transaction-bound execution.

### Phase 7: Batch CRUD Helpers

- Implement `InsertMany`, `InsertManyReturning`, and `SaveMany`.
- Accept slices or arrays of structs, pointers to structs, or maps.
- Reject nil, non-slice values, and empty batches with clear errors.
- Generate SQL once per chunk shape and bind each row without reparsing.
- Split large batches into chunks to stay under driver parameter limits; allow callers to override chunk rows with `BatchSize`.
- Test small batches, larger batches, map batches, pointer batches, empty batches, mixed missing map keys, save/upsert behavior, and transaction rollback on mid-batch errors.

### Phase 8: Prepared CRUD Plans

- Implement `PrepareInsert`, `PrepareSave`, and `CrudStmt`.
- Cache generated SQL, parameter order, and struct traversals.
- Add `Exec`, `ExecMany`, `ExecReturning`, and `Close`.
- Ensure prepared statements work on DB-backed and transaction-bound Engines.
- Test close behavior, repeated execution, batch execution, returning execution, and error propagation after prepare or bind failures.

### Phase 9: Performance Benchmarks

- Add benchmarks for raw sqlx calls versus `ExecP`, `GetP`, and `SelectP`.
- Add benchmarks for named calls one-shot versus prepared named statements.
- Add benchmarks for dynamic SQL one-shot versus prepared dynamic statements.
- Add benchmarks for `InsertMany`, `SaveMany`, `PrepareInsert.ExecMany`, and `PrepareSave.ExecMany`.
- Include small batch and larger batch sizes.

### Phase 10: Documentation and Compatibility

- Keep `demo.md` in sync with the implemented API.
- Update README or client-facing docs with the Engine-first model after implementation stabilizes.
- Document that client application code should use `Manager` plus `*Engine`; lower-level wrappers are implementation building blocks and migration-only surfaces.
- Document migration boundaries: SQL dialect translation is not provided for arbitrary SQL; CRUD helpers generate supported dialect SQL.
- Run `go test -v -count=1 ./...`; run race tests and lint when environment supports them.
- Race validation requires cgo and a C compiler on Windows. The current local environment has `CGO_ENABLED=0` and no `gcc`/`clang`, so race commands are documented but not executable here.

## Test Matrix

- SQLite single database: Engine lookup, base SQL, dynamic SQL, CRUD helpers, batch helpers, transactions.
- SQLite multi-database: independent Engine lookup and execution against separate stores.
- PostgreSQL bind mode: placeholder rebinding, named SQL, dynamic SQL, CRUD generated SQL, returning, upsert.
- Transaction-bound Engine: every Engine method used in `demo.md` works inside `WithTransaction`.
- Error paths: missing database, missing fields, invalid identifiers, empty batch, unsupported dialect, no rows, rollback on callback error, rollback on panic.
- Concurrency: repeated `GetEngine` lookup and cached metadata access under concurrent callers.

## Design Decisions

- `InsertManyReturning` is part of the first implementation for SQLite and PostgreSQL-compatible dialects. Unsupported dialects return `ErrUnsupportedDialect`.
- Prepared CRUD uses one generic public `CrudStmt` for both insert and save/upsert plans. The statement stores the prepared SQL, selected columns, and cached struct traversal metadata.
- `Engine(name)` remains as a panic-on-missing alias for `MustEngine(name)`.
- `DefaultEngine()` is provided for single-database applications that use the default `"app"` database.
- `OrderBy` remains a raw trusted SQL escape hatch. New application code should prefer validated `OrderAsc` and `OrderDesc`.
- `go.mod` keeps `go 1.25.0` because the requested pure-Go SQLite test driver, `modernc.org/sqlite v1.49.1`, declares `GoVersion: 1.25.0`.
- `Manager.DB`, `Manager.MustDB`, and `DB.LazyEngine` are removed from the client-facing API so applications use `Manager.GetEngine`, `Manager.MustEngine`, `Manager.Engine`, or `Manager.DefaultEngine`.
- Lower-level `DB` and `Tx` methods remain only as Engine implementation building blocks and migration-only surfaces; README no longer documents them as standard client API.

## Documentation Updates

`demo.md` is the current client-facing design document and has been kept in sync with the implemented API. A README update can be done after final review with:

- `db.GetEngine("dbName")` for named databases and `db.DefaultEngine()` for single-database applications.
- Single database setup.
- Multi-database setup.
- Simple positional query examples.
- Named parameter examples.
- Dynamic SQL examples.
- CRUD-style examples using `engine.Insert`, `engine.InsertMany`, `engine.SaveMany`, `engine.Update`, `engine.Delete`, `engine.GetBy`, and `engine.SelectBy`.
- Transaction examples where callback receives `*sqlx.Engine`.
- Performance guidance for caches, prepared statements, and batch operations.
- Warning that raw `DB` or `Tx` calls are not the standard client API.

## Acceptance Criteria

- [x] `demo.md` examples compile against the implemented public API or are explicitly marked as design-only before implementation.
- [x] `db.GetEngine("dbName")` and `db.DefaultEngine()` are the unified client-facing entrypoints.
- [x] Single database, multi-database, and tenant database examples use the same Engine lookup model.
- [x] Repository code can depend only on `*sqlx.Engine`.
- [x] Transaction callbacks receive a transaction-bound `*sqlx.Engine` with the same public methods as the normal Engine.
- [x] Engine supports positional SQL, named SQL, dynamic SQL, CRUD helper calls, batch insert/save, and prepared CRUD hot paths.
- [x] SQLite-to-PostgreSQL migration requires no repository code changes when callers use Engine CRUD helpers or canonical SQL rules and avoid arbitrary dialect-specific SQL.
- [x] CRUD helpers validate identifiers and bind values safely.
- [x] Batch helpers avoid per-row SQL parsing and repeated reflection.
- [x] Unsupported dialect/helper combinations return clear errors.
- [x] Tests cover success and error paths for Manager lookup, base Engine methods, dynamic SQL, CRUD helpers, batch helpers, prepared CRUD, and transactions.
- [x] Benchmarks document one-shot versus prepared performance for dynamic SQL and CRUD batch operations.
