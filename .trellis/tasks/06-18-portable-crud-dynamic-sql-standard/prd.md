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
```

Recommended semantics:

- `GetEngine(name)` returns `(*Engine, error)`.
- `MustEngine(name)` panics on missing database and is useful during initialization.
- `Engine(name)` may be considered as a short alias if the project accepts panic-on-missing semantics; otherwise prefer `MustEngine`.
- `name == ""` maps to the default database, usually `"app"`.
- Each database name returns a stable, cached Engine bound to that database.

For a single database application, the same pattern still applies:

```go
db := sqlx.NewManager()
db.MustOpen("app", "sqlite3", dsn)
app := db.MustEngine("app")
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
- CRUD-style operations through `ExecNamed`, `GetNamed`, `SelectNamed`, `Select`, and `ExecP`.
- `WithTransaction` for transaction callbacks that receive a transaction-bound Engine.
- Context-aware variants.
- Prepared dynamic statements for hot paths.

This means repository code depends on `*sqlx.Engine`, not `*sqlx.DB`, `*sqlx.Tx`, or `Runner`.

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

## CRUD-Style Calls

Simple CRUD should be expressed with explicit SQL and Engine methods. The client should not need to learn a separate table/model DSL.

```go
_, err := app.ExecNamed(`
    INSERT INTO users (name, email, status)
    VALUES (:name, :email, :status)
`, user)

err = app.GetNamed(&user, `
    SELECT id, name, email, status
    FROM users
    WHERE id = :id
`, map[string]any{"id": id})

_, err = app.ExecNamed(`
    UPDATE users
    SET name = :name, email = :email, status = :status
    WHERE id = :id
`, user)

_, err = app.ExecP("DELETE FROM users WHERE id = ?", id)
```

The standard should provide examples and optional helpers for common CRUD-style SQL, but the primary API remains Engine methods:

- Create: `ExecNamed` or `GetNamed` when an ID is returned.
- Read one: `GetP`, `GetNamed`, or dynamic `Get`.
- Read many: `SelectP`, `SelectNamed`, or dynamic `Select`.
- Update: `ExecNamed`.
- Delete: `ExecP` or `ExecNamed`.

Optional code generation or helper packages may be considered later, but they should not replace the unified Engine method surface.

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

- `GetEngine(name)` should be a cheap lookup returning a cached Engine.
- `Engine` should cache bind type and database metadata.
- Dynamic SQL templates should be cacheable.
- Prepared named and dynamic statements should cache parsed SQL and parameter metadata.
- Prepared dynamic statements should be available for hot paths.
- Batch insert should avoid per-row SQL parsing.
- Transaction-bound Engine should not add extra reflection or parsing beyond the selected query API.

Benchmarks should compare:

- Raw sqlx calls versus `ExecP`, `GetP`, `SelectP`.
- Named calls with and without prepared/cached templates.
- Dynamic SQL one-shot calls versus prepared dynamic statements.
- Prepared statement cold start versus warm cache.
- Batch insert behavior for small and large slices.

## Documentation Updates

Update client-facing docs with:

- `db.GetEngine("dbName")` as the single recommended entrypoint.
- Single database setup.
- Multi-database setup.
- Simple positional query examples.
- Named parameter examples.
- Dynamic SQL examples.
- CRUD-style examples using `engine.ExecNamed`, `engine.GetNamed`, `engine.Select`, and `engine.ExecP`.
- Transaction examples where callback receives `*sqlx.Engine`.
- Performance guidance for caches, prepared statements, and batch operations.
- Warning that raw `DB` or `Tx` calls are lower-level escape hatches.

## Acceptance Criteria

- [ ] The final design makes `db.GetEngine("dbName")` the unified client-facing entrypoint.
- [ ] Single database and multi-database examples use the same mental model.
- [ ] Repository code depends on `*sqlx.Engine`.
- [ ] Transaction callbacks receive a transaction-bound `*sqlx.Engine`.
- [ ] Engine supports positional SQL, named SQL, dynamic SQL, CRUD-style calls through Engine methods, and prepared hot paths.
- [ ] SQLite-to-PostgreSQL migration requires no repository SQL changes when the repository follows the standard and avoids dialect-specific SQL.
- [ ] Performance expectations and benchmark targets are documented.
