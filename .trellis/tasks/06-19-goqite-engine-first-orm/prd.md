# goqite Engine-first ORM Integration Analysis

## Goal

Analyze how applications can use this Engine-first ORM and introduce `maragu.dev/goqite` as a message queue with low ceremony, especially for SQLite applications using `github.com/mattn/go-sqlite3`.

## What I Already Know

* This project positions `*sqlx.Engine` as the application-facing repository dependency.
* goqite v0.4.0 creates queues from a standard `*sql.DB`.
* goqite supports SQLite and PostgreSQL and uses `goqite.SQLFlavorPostgreSQL` for PostgreSQL.
* goqite v0.4.0 requires `github.com/mattn/go-sqlite3 v1.14.28`.
* This project currently requires `github.com/mattn/go-sqlite3 v1.14.22`.
* This project can technically expose `*sql.DB` through `engine.DB().DB`, but that path is not documented as application-facing.
* goqite has transaction-aware methods such as `SendTx` and `jobs.CreateTx`, but they require a standard `*sql.Tx`.

## Requirements

* Identify project changes that reduce goqite adoption friction without breaking the Engine-first application model.
* Avoid forcing all sqlx users to depend on goqite unless a later decision explicitly accepts that trade-off.
* Support SQLite-first applications, while keeping PostgreSQL compatibility in mind because both projects support it.
* Preserve clear boundaries between ORM/repository APIs and external queue libraries.
* Document the recommended setup for driver, DSN, schema, queue creation, and optional transaction integration.

## Acceptance Criteria

* [x] The analysis identifies the minimum API additions needed for goqite to consume a DB managed by this ORM.
* [x] The analysis distinguishes simple queue usage from atomic domain-write-plus-enqueue usage.
* [x] The analysis accounts for sqlite3 version compatibility with goqite v0.4.0.
* [x] The analysis calls out SQLite DSN and pool settings relevant to goqite.
* [x] The analysis recommends whether to add docs only, core accessors, transaction bridge APIs, or an optional adapter package.

## Definition of Done

* Research notes are stored under `research/`.
* PRD records the recommended direction and open decisions.
* No production code is changed during this analysis task unless the task is later activated for implementation.

## Technical Approach

Recommended MVP:

1. Add stable standard handle accessors, such as `Engine.StdDB()` and possibly `Manager.StdDB(name)` or `Manager.GetDB(name)`.
2. Document goqite setup using the same DB connection managed by sqlx.
3. Update `github.com/mattn/go-sqlite3` to v1.14.28 to align with goqite and reduce dependency-resolution surprises.
4. Add a short integration recipe showing SQLite schema setup and queue construction.

Recommended follow-up if atomicity is required:

1. Add an Engine-level transaction bridge that exposes both the transaction-bound `*Engine` and the underlying `*sql.Tx`.
2. Document using `goqite.SendTx` or `jobs.CreateTx` inside that callback.

## Decision Options

### Option A: Accessors and Documentation (Recommended MVP)

Expose standard DB handles intentionally, then document goqite setup. This keeps sqlx independent from goqite while making the escape hatch official.

### Option B: Accessors plus Transaction Bridge

Do Option A and add a raw transaction bridge for atomic job enqueue. This is stronger for real application workflows but expands the Engine API.

Selected.

### Option C: Optional goqite Adapter

Add a helper package for goqite. This is most convenient but creates a tighter external integration and should be deferred until repeated demand exists.

## Out of Scope

* Reimplementing message queue behavior inside this ORM.
* Making goqite a required dependency of the root module in the MVP.
* Guaranteeing cross-database queue portability beyond what goqite already supports.
* Building a migration framework solely for goqite schemas.

## Research References

* [`research/goqite-integration.md`](research/goqite-integration.md) - goqite API shape, dependency version, transaction hooks, and sqlx integration friction.

## Technical Notes

* goqite GitHub: https://github.com/maragudk/goqite
* goqite latest inspected locally: `maragu.dev/goqite v0.4.0`
* Relevant sqlx files inspected: `manager.go`, `engine.go`, `sqlx.go`, `README.md`, `go.mod`
* Current project branch: `feature-api-v2`

## Open Questions

* None.

## Decision (ADR-lite)

**Context**: goqite consumes standard `*sql.DB` / `*sql.Tx`, while this project encourages application repositories to depend on `*sqlx.Engine`. Without a supported bridge, applications either use the discouraged `engine.DB().DB` path or leave Engine-managed transactions for queue integration.

**Decision**: Implement Option B: add official standard DB accessors plus an Engine transaction bridge that exposes both transaction-bound `*Engine` and standard `*sql.Tx`.

**Consequences**: Applications can initialize goqite from sqlx-managed connections and atomically enqueue messages with domain writes. The API must make the lower-level escape hatch intentional and documented, without weakening the default Engine-first guidance.
