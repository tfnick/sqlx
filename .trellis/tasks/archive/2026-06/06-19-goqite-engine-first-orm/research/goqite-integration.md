# goqite Integration Research

## Sources

* GitHub repository: https://github.com/maragudk/goqite
* Local module cache inspected via `go list -m -json maragu.dev/goqite@latest`
* Version inspected: `maragu.dev/goqite v0.4.0`

## goqite Facts

* `goqite.New` accepts `goqite.NewOpts{DB: *sql.DB, Name: string, SQLFlavor: ...}`.
* SQLite is the default SQL flavor; PostgreSQL uses `goqite.SQLFlavorPostgreSQL`.
* The module currently requires `github.com/mattn/go-sqlite3 v1.14.28`.
* goqite intentionally has no non-test SQL driver dependency at runtime; applications bring and register their driver.
* Queue operations start their own `*sql.Tx` at `sql.LevelSerializable`, unless the caller uses `SendTx`, `ReceiveTx`, `ExtendTx`, or `DeleteTx`.
* The jobs package provides `jobs.CreateTx`, which also requires a standard `*sql.Tx`.
* Current goqite source uses a `priority` column. The package test schemas include `priority integer not null default 0` and `goqite_queue_priority_created_idx`; the rendered README schema may lag behind this.

## Current sqlx Facts

* The project is Engine-first: application repositories are expected to depend on `*sqlx.Engine`.
* `Manager.Open` accepts driver name and DSN, calls `sqlx.Open`, and caches `*Engine` per named DB.
* `Manager.Add` can register an existing `*sqlx.DB`; `sqlx.NewDb(raw *sql.DB, driverName string)` can wrap an existing standard DB.
* `Engine.DB()` exposes `*sqlx.DB`, but it is documented as an internal/test integration escape hatch.
* `*sqlx.DB` embeds `*sql.DB`, so goqite can technically use `engine.DB().DB`, but this is awkward and pushes users toward a discouraged lower-level handle.
* Transaction-bound `*Engine` wraps `*sqlx.Tx`; there is no Engine-level API to expose a standard `*sql.Tx` for libraries such as goqite.
* The module currently requires `github.com/mattn/go-sqlite3 v1.14.22`, while goqite v0.4.0 requires v1.14.28.
* README examples prefer `modernc.org/sqlite` with driver name `sqlite`, while older demo/docs mention `github.com/mattn/go-sqlite3` driver name `sqlite3`.

## Integration Friction

1. Applications must know how to get from `*sqlx.Engine` to standard `*sql.DB`.
2. The current escape hatch is discoverable but not application-facing: `engine.DB().DB`.
3. A domain transaction that also enqueues a goqite job needs access to the same standard `*sql.Tx`; Engine-first code does not expose that directly.
4. SQLite DSN and pool settings matter. goqite examples use mattn sqlite3 with WAL, timeout, foreign keys, and often one open/idle connection for in-memory examples.
5. Docs should distinguish same DB queue table, separate queue DB, and PostgreSQL flavor deployments.
6. Schema setup should not copy stale README snippets blindly; the package-provided schema has priority support.

## Feasible Improvements

### Approach A: Raw Standard Handle Accessors and Docs (Recommended MVP)

Add stable, application-facing escape hatches:

* `func (e *Engine) StdDB() *sql.DB`
* `func (db *DB) StdDB() *sql.DB`
* `func (m *Manager) GetDB(name string) (*DB, error)` or `StdDB(name string) (*sql.DB, error)`

Then document creating goqite from `app.StdDB()`.

Pros:

* Minimal dependency surface; sqlx does not import goqite.
* Matches goqite's "bring your own driver" philosophy.
* Keeps the Engine-first repository rule intact while acknowledging integration boundaries.

Cons:

* Does not solve same-transaction enqueue by itself.

### Approach B: Transaction Bridge

Add an Engine transaction helper that exposes both the Engine and standard transaction:

```go
func (e *Engine) WithTransactionRaw(ctx context.Context, opts *sql.TxOptions, fn func(tx *Engine, raw *sql.Tx) error) error
```

Pros:

* Enables reliable domain write plus enqueue atomicity.
* Avoids requiring users to abandon Engine-first repositories.

Cons:

* Exposes lower-level transaction details and must define behavior for nested transaction engines.
* Requires careful naming to avoid implying all users should use raw handles.

### Approach C: Optional Adapter Subpackage

Create an optional subpackage, for example `goqitex`, that imports `maragu.dev/goqite` and provides queue constructors from `*Engine` or `*Manager`.

Pros:

* Most convenient application UX.
* Can centralize SQL flavor detection and defaults.

Cons:

* Pulls goqite into this module's dependency graph for users who do not need queues, unless split carefully.
* Adds maintenance burden for an external library API.

## Recommended Direction

Start with Approach A plus documentation. Add Approach B if the desired promise includes atomic enqueue inside ORM-managed transactions. Avoid Approach C initially unless this project explicitly wants first-class queue integration as product surface rather than low-friction interoperability.
