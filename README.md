# sqlx

[![CircleCI](https://dl.circleci.com/status-badge/img/gh/tfnick/sqlx/tree/master.svg?style=shield)](https://dl.circleci.com/status-badge/redirect/gh/tfnick/sqlx/tree/master) [![Coverage Status](https://coveralls.io/repos/github/tfnick/sqlx/badge.svg?branch=master)](https://coveralls.io/github/tfnick/sqlx?branch=master) [![Godoc](http://img.shields.io/badge/godoc-reference-blue.svg?style=flat)](https://godoc.org/github.com/tfnick/sqlx) [![license](http://img.shields.io/badge/license-MIT-red.svg?style=flat)](https://raw.githubusercontent.com/tfnick/sqlx/master/LICENSE)

sqlx provides an Engine-first SQL API for Go applications.  The goal is simple:
application code gets a `*sqlx.Engine` first, then uses that Engine for queries,
dynamic SQL, CRUD helpers, batch writes, prepared write plans, and transactions.

This README is written from the client application's point of view.  If an
application follows these calling rules, moving simple repository code between
SQLite and PostgreSQL is mostly a configuration change: driver, DSN, and database
setup change, while repository methods keep the same Engine API.

## Install

```sh
go get github.com/tfnick/sqlx
```

## Core Model

Only remember one entrypoint:

```go
engine, err := db.GetEngine("app")
if err != nil {
    return err
}
```

Single-database applications can use the default database name, `"app"`:

```go
app := db.DefaultEngine()
```

Multi-database applications choose the database once when wiring repositories:

```go
app, err := db.GetEngine("app")
if err != nil {
    return err
}

logs, err := db.GetEngine("logs")
if err != nil {
    return err
}
```

Repository code should depend on `*sqlx.Engine`, not `*sqlx.DB`, `*sqlx.Tx`, or
driver-specific handles.

## Open Stores

Register databases during application startup.  The repository layer stores only
`*sqlx.Engine`.

SQLite:

```go
package app

import (
    "github.com/tfnick/sqlx"

    _ "modernc.org/sqlite"
)

func OpenSQLiteStores() (*sqlx.Manager, error) {
    db := sqlx.NewManager()

    if err := db.Open("app", "sqlite", "file:app.db?_pragma=foreign_keys(1)"); err != nil {
        return nil, err
    }
    if err := db.Open("logs", "sqlite", "file:logs.db?_pragma=foreign_keys(1)"); err != nil {
        _ = db.Close()
        return nil, err
    }

    return db, nil
}
```

PostgreSQL:

```go
package app

import (
    "github.com/tfnick/sqlx"

    _ "github.com/lib/pq"
)

func OpenPostgresStores() (*sqlx.Manager, error) {
    db := sqlx.NewManager()

    if err := db.Open("app", "postgres", "postgres://user:pass@localhost/app?sslmode=disable"); err != nil {
        return nil, err
    }
    if err := db.Open("logs", "postgres", "postgres://user:pass@localhost/logs?sslmode=disable"); err != nil {
        _ = db.Close()
        return nil, err
    }

    return db, nil
}
```

## Models And Repositories

Use `db` tags for column mapping.  Named parameters, dynamic SQL, and CRUD
helpers all use the same mapping.

```go
type User struct {
    ID        int64  `db:"id"`
    Name      string `db:"name"`
    Email     string `db:"email"`
    Age       int    `db:"age"`
    Status    string `db:"status"`
    CreatedAt string `db:"created_at"`
    UpdatedAt string `db:"updated_at"`
}

type UserQuery struct {
    Name   string  `db:"name"`
    Email  string  `db:"email"`
    Status string  `db:"status"`
    MinAge *int    `db:"min_age"`
    MaxAge *int    `db:"max_age"`
    IDs    []int64 `db:"ids"`
    Limit  int     `db:"limit"`
    Offset int     `db:"offset"`
}
```

Repositories receive only `*sqlx.Engine`:

```go
type UserRepo struct {
    e *sqlx.Engine
}

func NewUserRepo(e *sqlx.Engine) *UserRepo {
    return &UserRepo{e: e}
}

func NewUserRepoFromDB(db *sqlx.Manager) (*UserRepo, error) {
    app, err := db.GetEngine("app")
    if err != nil {
        return nil, err
    }
    return NewUserRepo(app), nil
}
```

## Positional SQL

Use `ExecP`, `GetP`, and `SelectP` for simple positional SQL.  Application SQL
uses `?`; Engine rebinding adapts it to the current driver.

```go
func (r *UserRepo) FindByID(id int64) (User, error) {
    var user User
    err := r.e.GetP(&user, `
        SELECT id, name, email, age, status, created_at, updated_at
        FROM users
        WHERE id = ?
    `, id)
    return user, err
}

func (r *UserRepo) FindByStatus(status string, limit int) ([]User, error) {
    var users []User
    err := r.e.SelectP(&users, `
        SELECT id, name, email, age, status, created_at, updated_at
        FROM users
        WHERE status = ?
        ORDER BY id DESC
        LIMIT ?
    `, status, limit)
    return users, err
}
```

## Named SQL

Use `ExecNamed`, `GetNamed`, and `SelectNamed` when parameter order would be
annoying or error-prone.

```go
func (r *UserRepo) FindByEmail(email string) (User, error) {
    var user User
    err := r.e.GetNamed(&user, `
        SELECT id, name, email, age, status, created_at, updated_at
        FROM users
        WHERE email = :email
    `, map[string]any{
        "email": email,
    })
    return user, err
}

func (r *UserRepo) CreateByNamed(user User) error {
    _, err := r.e.ExecNamed(`
        INSERT INTO users (name, email, age, status)
        VALUES (:name, :email, :age, :status)
    `, user)
    return err
}
```

## Dynamic SQL

Use Engine dynamic SQL when conditions are optional.  A `#[ ... ]` block is kept
only when at least one named parameter inside the block has a meaningful value.

```go
func (r *UserRepo) Search(q UserQuery) ([]User, error) {
    var users []User
    err := r.e.Select(&users, `
        SELECT id, name, email, age, status, created_at, updated_at
        FROM users
        WHERE 1=1
        #[ AND name LIKE :name ]
        #[ AND email = :email ]
        #[ AND status = :status ]
        #[ AND age >= :min_age ]
        #[ AND age <= :max_age ]
        ORDER BY id DESC
    `, q)
    return users, err
}
```

Callers only fill the filters they need:

```go
minAge := 18

users, err := repo.Search(UserQuery{
    Name:   "%tom%",
    Status: "active",
    MinAge: &minAge,
})
```

## IN Slices

Use `IN :ids` for slice parameters.  Empty slices remove the optional condition;
non-empty slices expand and rebind for the current database.

```go
func (r *UserRepo) FindByIDs(ids []int64) ([]User, error) {
    var users []User
    err := r.e.Select(&users, `
        SELECT id, name, email, age, status, created_at, updated_at
        FROM users
        WHERE 1=1
        #[ AND id IN :ids ]
        ORDER BY id
    `, UserQuery{
        IDs: ids,
    })
    return users, err
}
```

Multiple slice filters use the same pattern:

```go
type OrderQuery struct {
    UserIDs  []int64  `db:"user_ids"`
    Statuses []string `db:"statuses"`
}

func (r *OrderRepo) Search(q OrderQuery) ([]Order, error) {
    var orders []Order
    err := r.e.Select(&orders, `
        SELECT id, user_id, status, amount, created_at
        FROM orders
        WHERE 1=1
        #[ AND user_id IN :user_ids ]
        #[ AND status IN :statuses ]
        ORDER BY id DESC
    `, q)
    return orders, err
}
```

## Pagination

The list query and the count query can share the same query object.

```go
func (r *UserRepo) Page(q UserQuery) ([]User, int, error) {
    var users []User
    if err := r.e.Select(&users, `
        SELECT id, name, email, age, status, created_at, updated_at
        FROM users
        WHERE 1=1
        #[ AND name LIKE :name ]
        #[ AND status = :status ]
        ORDER BY id DESC
        LIMIT :limit OFFSET :offset
    `, q); err != nil {
        return nil, 0, err
    }

    var total int
    if err := r.e.Get(&total, `
        SELECT count(*)
        FROM users
        WHERE 1=1
        #[ AND name LIKE :name ]
        #[ AND status = :status ]
    `, q); err != nil {
        return nil, 0, err
    }

    return users, total, nil
}
```

## CRUD Helpers

For simple single-table operations, call Engine CRUD helpers instead of writing
vendor-specific SQL in repositories.

```go
_, err := app.Insert("users", user,
    sqlx.Columns("name", "email", "age", "status"),
)
```

Return generated IDs:

```go
var id int64
err := app.InsertReturning(&id, "users", user,
    sqlx.Columns("name", "email", "age", "status"),
    sqlx.Returning("id"),
)
```

Update explicit columns by explicit keys:

```go
_, err := app.Update("users", user,
    sqlx.Keys("id"),
    sqlx.Columns("name", "email", "age"),
)
```

Delete and read by predicates:

```go
_, err := app.Delete("users", sqlx.Where("id", id))
```

```go
var user User
err := app.GetBy(&user, "users",
    sqlx.Where("id", id),
    sqlx.Where("status", "active"),
    sqlx.Columns("id", "name", "email", "age", "status", "created_at", "updated_at"),
)
```

Simple list reads can also use CRUD helpers:

```go
var users []User
err := app.SelectBy(&users, "users",
    sqlx.Where("status", "active"),
    sqlx.Columns("id", "name", "email", "age", "status", "created_at"),
    sqlx.OrderDesc("id"),
    sqlx.LimitOffset(limit, offset),
)
```

`Delete`, `GetBy`, and `SelectBy` require at least one `Where` unless
`AllowAllRows()` is passed explicitly.  Prefer `OrderAsc` and `OrderDesc` for
validated column ordering.  `OrderBy` is a raw trusted SQL escape hatch for
static application SQL.

## Batch Insert

Batch insert is an Engine feature, not a repository loop.  Engine generates
multi-row SQL, binds fields, rebinds placeholders, chunks large batches, and
caches field metadata for the batch.

```go
_, err := app.InsertMany("users", users,
    sqlx.Columns("name", "email", "age", "status"),
)
```

Use the returning variant when IDs are needed:

```go
var ids []int64
err := app.InsertManyReturning(&ids, "users", users,
    sqlx.Columns("name", "email", "age", "status"),
    sqlx.Returning("id"),
)
```

Batch insert works the same inside a transaction:

```go
err := app.WithTransaction(func(tx *sqlx.Engine) error {
    _, err := tx.InsertMany("users", users,
        sqlx.Columns("name", "email", "age", "status"),
    )
    return err
})
```

## Batch Save

`Save` and `SaveMany` mean upsert: insert when the conflict key does not exist,
update when it does.  SQLite and PostgreSQL use `ON CONFLICT (...) DO UPDATE`.

```go
_, err := app.Save("users", user,
    sqlx.ConflictKeys("email"),
    sqlx.InsertColumns("name", "email", "age", "status"),
    sqlx.UpdateColumns("name", "age", "status"),
)
```

```go
_, err := app.SaveMany("users", users,
    sqlx.ConflictKeys("email"),
    sqlx.InsertColumns("name", "email", "age", "status"),
    sqlx.UpdateColumns("name", "age", "status"),
)
```

Use primary keys as conflict keys when that is the save rule:

```go
_, err := app.SaveMany("users", users,
    sqlx.ConflictKeys("id"),
    sqlx.InsertColumns("id", "name", "email", "age", "status"),
    sqlx.UpdateColumns("name", "email", "age", "status"),
)
```

Unsupported dialect/helper combinations return `ErrUnsupportedDialect`.

## Prepared Write Plans

Hot write paths can prepare a CRUD plan once.  Preparation validates table and
column names, generates SQL for the current dialect, and caches struct field
metadata.  Execution only binds row data.

```go
insertUsers, err := app.PrepareInsert("users",
    sqlx.Columns("name", "email", "age", "status"),
)
if err != nil {
    return err
}
defer insertUsers.Close()

_, err = insertUsers.ExecMany(users)
```

```go
saveUsers, err := app.PrepareSave("users",
    sqlx.ConflictKeys("email"),
    sqlx.InsertColumns("name", "email", "age", "status"),
    sqlx.UpdateColumns("name", "age", "status"),
)
if err != nil {
    return err
}
defer saveUsers.Close()

_, err = saveUsers.ExecMany(users)
```

Returning values are also supported for prepared inserts:

```go
insertUser, err := app.PrepareInsert("users",
    sqlx.Columns("name", "email", "age", "status"),
    sqlx.Returning("id"),
)
if err != nil {
    return err
}
defer insertUser.Close()

var id int64
err = insertUser.ExecReturning(&id, user)
```

## Transactions

Transactions start from Engine.  The callback receives a transaction-bound
`*sqlx.Engine`, so repositories work the same inside and outside transactions.

```go
type AccountRepo struct {
    e *sqlx.Engine
}

func NewAccountRepo(e *sqlx.Engine) *AccountRepo {
    return &AccountRepo{e: e}
}

func (r *AccountRepo) AddBalance(accountID int64, delta int64) error {
    _, err := r.e.ExecNamed(`
        UPDATE accounts
        SET balance = balance + :delta
        WHERE id = :account_id
    `, map[string]any{
        "account_id": accountID,
        "delta":      delta,
    })
    return err
}

type AccountService struct {
    app *sqlx.Engine
}

func NewAccountService(db *sqlx.Manager) (*AccountService, error) {
    app, err := db.GetEngine("app")
    if err != nil {
        return nil, err
    }
    return &AccountService{app: app}, nil
}

func (s *AccountService) Transfer(fromID int64, toID int64, amount int64) error {
    return s.app.WithTransaction(func(tx *sqlx.Engine) error {
        repo := NewAccountRepo(tx)

        if err := repo.AddBalance(fromID, -amount); err != nil {
            return err
        }
        return repo.AddBalance(toID, amount)
    })
}
```

Use `WithTransactionContext` when cancellation, timeout, or isolation level is
needed:

```go
func (s *AccountService) TransferStrict(ctx context.Context, fromID int64, toID int64, amount int64) error {
    opts := &sql.TxOptions{
        Isolation: sql.LevelSerializable,
        ReadOnly:  false,
    }

    return s.app.WithTransactionContext(ctx, opts, func(tx *sqlx.Engine) error {
        repo := NewAccountRepo(tx)

        if err := repo.AddBalance(fromID, -amount); err != nil {
            return err
        }
        return repo.AddBalance(toID, amount)
    })
}
```

Dynamic SQL and batch helpers work on transaction Engines too:

```go
func (s *AccountService) ActivateUsers(ids []int64) error {
    return s.app.WithTransaction(func(tx *sqlx.Engine) error {
        _, err := tx.Exec(`
            UPDATE users
            SET status = :status
            WHERE 1=1
            #[ AND id IN :ids ]
        `, map[string]any{
            "status": "active",
            "ids":    ids,
        })
        return err
    })
}
```

## Integrating Message Queues

Some libraries need the standard `database/sql` handles rather than sqlx
wrappers.  For example, `maragu.dev/goqite` creates a persistent queue from a
`*sql.DB` and can enqueue messages inside an existing `*sql.Tx`.

Use `StdDB` when wiring the queue:

```go
package app

import (
    "github.com/tfnick/sqlx"

    "maragu.dev/goqite"
)

func NewJobQueue(db *sqlx.Manager) (*goqite.Queue, error) {
    app, err := db.GetEngine("app")
    if err != nil {
        return nil, err
    }

    return goqite.New(goqite.NewOpts{
        DB:   app.StdDB(),
        Name: "jobs",
    }), nil
}
```

For SQLite applications using `github.com/mattn/go-sqlite3`, use the `sqlite3`
driver name and a DSN configured for queue contention, for example:

```go
if err := db.Open("app", "sqlite3", "file:app.db?_journal=WAL&_timeout=5000&_fk=true"); err != nil {
    return nil, err
}
```

Install goqite's schema in the same database, or use a separate named database
when queue isolation is more important than sharing a transaction.

When a domain write and job enqueue must commit atomically, use
`WithTransactionRaw`.  Repository code still receives a transaction-bound
`*sqlx.Engine`, while goqite receives the standard `*sql.Tx` it requires:

```go
func (s *OrderService) CreateOrder(ctx context.Context, order Order, body []byte) error {
    return s.app.WithTransactionRaw(ctx, nil, func(tx *sqlx.Engine, rawTx *sql.Tx) error {
        if _, err := tx.ExecP(`
            INSERT INTO orders (customer_id, status, total)
            VALUES (?, ?, ?)
        `, order.CustomerID, order.Status, order.Total); err != nil {
            return err
        }

        return s.jobs.SendTx(ctx, rawTx, goqite.Message{
            Body: body,
        })
    })
}
```

Use `goqite.SQLFlavorPostgreSQL` when the underlying database is PostgreSQL.

## Multiple Databases

Pick the database when creating repositories.  Repository methods should not
re-select database names internally.

```go
type AccessLog struct {
    ID        int64  `db:"id"`
    UserID    int64  `db:"user_id"`
    Action    string `db:"action"`
    CreatedAt string `db:"created_at"`
}

type AccessLogRepo struct {
    e *sqlx.Engine
}

func NewAccessLogRepo(e *sqlx.Engine) *AccessLogRepo {
    return &AccessLogRepo{e: e}
}

func (r *AccessLogRepo) Write(log AccessLog) error {
    _, err := r.e.ExecNamed(`
        INSERT INTO access_logs (user_id, action, created_at)
        VALUES (:user_id, :action, :created_at)
    `, log)
    return err
}

func NewRepos(db *sqlx.Manager) (*UserRepo, *AccessLogRepo, error) {
    app, err := db.GetEngine("app")
    if err != nil {
        return nil, nil, err
    }

    logs, err := db.GetEngine("logs")
    if err != nil {
        return nil, nil, err
    }

    return NewUserRepo(app), NewAccessLogRepo(logs), nil
}
```

Tenant databases are just different Engine names:

```go
func NewTenantUserRepo(db *sqlx.Manager, tenantID string) (*UserRepo, error) {
    tenantDB := "tenant_" + tenantID

    engine, err := db.GetEngine(tenantDB)
    if err != nil {
        return nil, err
    }
    return NewUserRepo(engine), nil
}
```

## Migration Between SQLite And PostgreSQL

Keep database-specific decisions in configuration:

```go
type StoreConfig struct {
    Name   string
    Driver string
    DSN    string
}

func OpenStores(configs []StoreConfig) (*sqlx.Manager, error) {
    db := sqlx.NewManager()

    for _, cfg := range configs {
        if err := db.Open(cfg.Name, cfg.Driver, cfg.DSN); err != nil {
            _ = db.Close()
            return nil, err
        }
    }

    return db, nil
}
```

SQLite:

```go
configs := []StoreConfig{
    {Name: "app", Driver: "sqlite", DSN: "file:app.db?_pragma=foreign_keys(1)"},
    {Name: "logs", Driver: "sqlite", DSN: "file:logs.db?_pragma=foreign_keys(1)"},
}
```

PostgreSQL:

```go
configs := []StoreConfig{
    {Name: "app", Driver: "postgres", DSN: "postgres://user:pass@localhost/app?sslmode=disable"},
    {Name: "logs", Driver: "postgres", DSN: "postgres://user:pass@localhost/logs?sslmode=disable"},
}
```

Migration-friendly repository rules:

* Use `Manager.GetEngine`, `Manager.MustEngine`, `Manager.Engine`, or `Manager.DefaultEngine`.
* Store `*sqlx.Engine` in repositories.
* Use `:name` for named parameters.
* Use `#[ ... ]` for optional dynamic SQL.
* Use `IN :ids` for slice parameters.
* Use `?` only with `ExecP`, `GetP`, and `SelectP`.
* Use Engine CRUD helpers for simple single-table insert, update, delete, select, batch insert, and upsert.
* Keep unavoidable vendor-specific SQL in explicitly named driver-specific methods.

## Recommended Choices

Use this quick guide when choosing an API:

| Situation | Use |
| --- | --- |
| Get the database execution surface | `db.GetEngine("app")` |
| Single database app | `db.DefaultEngine()` |
| One or two positional params | `GetP`, `SelectP`, `ExecP` |
| Many params or structs | `GetNamed`, `SelectNamed`, `ExecNamed` |
| Optional filters or `IN` slices | `Get`, `Select`, `Exec` dynamic SQL |
| Simple single-table write/read | `Insert`, `Update`, `Delete`, `GetBy`, `SelectBy` |
| Batch insert | `InsertMany`, `InsertManyReturning` |
| Upsert/save | `Save`, `SaveMany` |
| Hot write loop | `PrepareInsert`, `PrepareSave` |
| Atomic multi-step write | `WithTransaction`, `WithTransactionContext` |

## Not Recommended

Do not bypass Engine in application code:

```go
err := rawDB.Get(&user, "SELECT * FROM users WHERE id = $1", id)
```

Do not rely on raw `?` placeholders outside `ExecP`, `GetP`, or `SelectP`:

```go
err := rawDB.Get(&user, "SELECT * FROM users WHERE id = ?", id)
```

Do not build optional SQL by string concatenation:

```go
query := "SELECT * FROM users WHERE 1=1"
if status != "" {
    query += " AND status = '" + status + "'"
}
```

Use dynamic SQL blocks instead.

## Complete Example

```go
func Run() error {
    db, err := OpenSQLiteStores()
    if err != nil {
        return err
    }
    defer db.Close()

    app, err := db.GetEngine("app")
    if err != nil {
        return err
    }

    userRepo := NewUserRepo(app)
    users, err := userRepo.Search(UserQuery{
        Status: "active",
        Limit:  20,
    })
    if err != nil {
        return err
    }

    _ = users

    service := &AccountService{app: app}
    return service.Transfer(1, 2, 100)
}
```

## Testing

The Engine CRUD tests use `modernc.org/sqlite`, so the default CRUD tests do not
require cgo:

```sh
go test ./...
go vet ./...
```

PostgreSQL integration tests use the existing `SQLX_POSTGRES_DSN` environment
variable:

```sh
SQLX_POSTGRES_DSN='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' \
SQLX_MYSQL_DSN=skip \
SQLX_SQLITE_DSN=skip \
go test -count=1 ./...
```

Race tests require cgo and a working C compiler on Windows:

```sh
CGO_ENABLED=1 \
SQLX_POSTGRES_DSN='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' \
SQLX_MYSQL_DSN=skip \
SQLX_SQLITE_DSN=skip \
go test -race -run 'TestEngine|TestPrepared|TestManager' -count=1 ./...
```

SQLite file-mode and PostgreSQL throughput comparison:

```sh
SQLX_POSTGRES_DSN='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' \
SQLX_MYSQL_DSN=skip \
SQLX_SQLITE_DSN=skip \
go test -run '^$' -bench 'BenchmarkEngineCompare' -benchtime=1s -count=3 ./...
```
