# 客户端调用示例

本文从客户端应用的视角展示一套新的可迁移 sqlx 调用方式。目标是在不考虑 SQL 方言差异的前提下，让应用先使用 SQLite，后续切换到 PostgreSQL 时尽量不修改业务 SQL。

说明：本文中的 `GetEngine`、`ExecP`、`GetP`、`SelectP`、`ExecNamed`、`GetNamed`、`SelectNamed`、`Select`、`Exec`、`WithTransaction` 等 API 是新标准方案的设计示例，代表期望的客户端调用体验。

## 核心心智模型

客户端只记住一个入口：先通过数据库管理器拿到某个数据库的 Engine，然后所有查询、动态参数、增删改查、事务都从 Engine 继续调用。

```go
engine, err := db.GetEngine("app")
if err != nil {
    return err
}
```

单库应用也使用同一个入口，默认库名可以约定为 `"app"`。

```go
app, err := db.GetEngine("app")
if err != nil {
    return err
}
```

多库应用只是库名不同。

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

## 初始化

应用启动时注册一个或多个数据库。仓储层不直接保存 `*sqlx.DB`，而是保存从 `GetEngine` 得到的 `*sqlx.Engine`。

```go
package app

import (
    "github.com/tfnick/sqlx"

    _ "github.com/mattn/go-sqlite3"
)

func OpenSQLiteStores() (*sqlx.Manager, error) {
    db := sqlx.NewManager()

    if err := db.Open("app", "sqlite3", "file:app.db?_foreign_keys=on"); err != nil {
        return nil, err
    }
    if err := db.Open("logs", "sqlite3", "file:logs.db?_foreign_keys=on"); err != nil {
        _ = db.Close()
        return nil, err
    }

    return db, nil
}
```

切换到 PostgreSQL 时，调用方仍然从 `GetEngine("app")` 开始，变化集中在驱动和 DSN。

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

## 通用模型

模型通过 `db` tag 声明字段和列名映射。命名参数和动态 SQL 都复用这套映射。

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

## 创建仓储对象

仓储对象只接收 `*sqlx.Engine`。它不关心这个 Engine 来自 SQLite、PostgreSQL、普通连接还是事务。

```go
type UserRepo struct {
    e *sqlx.Engine
}

func NewUserRepo(e *sqlx.Engine) *UserRepo {
    return &UserRepo{e: e}
}
```

应用组装仓储对象时，统一通过 `GetEngine`。

```go
func NewUserRepoFromDB(db *sqlx.Manager) (*UserRepo, error) {
    app, err := db.GetEngine("app")
    if err != nil {
        return nil, err
    }
    return NewUserRepo(app), nil
}
```

## 简单查询

简单位置参数查询使用 `GetP`、`SelectP`、`ExecP`。客户端写 `?`，Engine 内部按当前数据库自动转换占位符。

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
```

列表查询保持同样写法。

```go
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

简单写入使用 `ExecP`。

```go
func (r *UserRepo) Delete(id int64) error {
    _, err := r.e.ExecP(`
        DELETE FROM users
        WHERE id = ?
    `, id)
    return err
}
```

## 命名参数

参数较多时使用命名参数，调用方不需要维护参数顺序。

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
```

命名参数也可以直接绑定结构体。

```go
func (r *UserRepo) CreateByNamed(user User) error {
    _, err := r.e.ExecNamed(`
        INSERT INTO users (name, email, age, status)
        VALUES (:name, :email, :age, :status)
    `, user)
    return err
}
```

## 动态条件查询

条件不固定时，直接使用 Engine 的动态 SQL 能力。`#[ ... ]` 中引用的参数为空时，该条件块会被移除。

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

调用方只传需要筛选的字段。

```go
minAge := 18

users, err := repo.Search(UserQuery{
    Name:   "%tom%",
    Status: "active",
    MinAge: &minAge,
})
```

## IN 查询

切片参数交给 Engine 展开。空切片会移除整个条件块，非空切片会展开并自动适配当前数据库占位符。

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

多个切片条件也是同样写法。

```go
type OrderQuery struct {
    UserIDs  []int64  `db:"user_ids"`
    Statuses []string `db:"statuses"`
}

type OrderRepo struct {
    e *sqlx.Engine
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

## 分页查询

分页查询可以复用同一个查询对象。列表和总数都从同一个 Engine 调用。

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

## 简单增删改查

标准单表操作仍然使用 Engine 方法，不引入额外的表模型链式封装。创建记录时，参数多且字段含义明确，推荐使用 `ExecNamed`。

```go
func (r *UserRepo) Create(user User) error {
    _, err := r.e.ExecNamed(`
        INSERT INTO users (name, email, age, status)
        VALUES (:name, :email, :age, :status)
    `, user)
    return err
}
```

如果创建后需要返回 ID，可以使用 `GetNamed` 承接返回值。具体 SQL 是否使用返回子句属于方言问题，这里只展示调用形态。

```go
func (r *UserRepo) CreateReturningID(user User) (int64, error) {
    var id int64
    err := r.e.GetNamed(&id, `
        INSERT INTO users (name, email, age, status)
        VALUES (:name, :email, :age, :status)
        RETURNING id
    `, user)
    return id, err
}
```

更新时显式写出要更新的列，避免零值字段被意外写入。

```go
func (r *UserRepo) UpdateProfile(user User) error {
    _, err := r.e.ExecNamed(`
        UPDATE users
        SET name = :name, email = :email, age = :age
        WHERE id = :id
    `, user)
    return err
}
```

删除记录可以使用 `ExecP`，保持简单。

```go
func (r *UserRepo) DeleteByID(id int64) error {
    _, err := r.e.ExecP(`
        DELETE FROM users
        WHERE id = ?
    `, id)
    return err
}
```

查询单条记录可以使用 `GetNamed`。

```go
func (r *UserRepo) GetActiveByID(id int64) (User, error) {
    var user User
    err := r.e.GetNamed(&user, `
        SELECT id, name, email, age, status, created_at, updated_at
        FROM users
        WHERE id = :id AND status = :status
    `, map[string]any{
        "id":     id,
        "status": "active",
    })
    return user, err
}
```

查询多条记录可以使用 `SelectNamed`，如果存在可选条件则使用 `Select`。

```go
func (r *UserRepo) ListActive(limit int, offset int) ([]User, error) {
    var users []User
    err := r.e.SelectNamed(&users, `
        SELECT id, name, email, age, status, created_at
        FROM users
        WHERE status = :status
        ORDER BY id DESC
        LIMIT :limit OFFSET :offset
    `, map[string]any{
        "status": "active",
        "limit":  limit,
        "offset": offset,
    })
    return users, err
}
```

## 批量插入

批量插入仍然通过 Engine 方法完成。库侧应缓存命名 SQL 的解析结果，避免每行重复解析。

```go
func (r *UserRepo) CreateMany(users []User) error {
    _, err := r.e.ExecNamed(`
        INSERT INTO users (name, email, age, status)
        VALUES (:name, :email, :age, :status)
    `, users)
    return err
}
```

如果批量写入是热点路径，可以提前准备命名语句。

```go
type BatchUserRepo struct {
    createUser *sqlx.NamedStmt
}

func NewBatchUserRepo(e *sqlx.Engine) (*BatchUserRepo, error) {
    stmt, err := e.PrepareNamed(`
        INSERT INTO users (name, email, age, status)
        VALUES (:name, :email, :age, :status)
    `)
    if err != nil {
        return nil, err
    }

    return &BatchUserRepo{createUser: stmt}, nil
}

func (r *BatchUserRepo) CreateMany(users []User) error {
    _, err := r.createUser.Exec(users)
    return err
}
```

## 事务

事务也从 Engine 开始。事务回调拿到的 `tx` 仍然是 `*sqlx.Engine`，所以仓储对象可以直接复用。

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
```

服务层负责开启事务。

```go
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

## 带 Context 的事务

需要超时、取消或隔离级别时，继续从同一个 Engine 调用。

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

## 事务中的动态 SQL

事务内不需要换一套 API。`tx` 是事务绑定的 Engine，仍然可以调用动态 SQL。

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

## 多数据库读写

多个数据库时，调用入口仍然是 `GetEngine`。业务库和日志库可以分别创建仓储对象。

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

跨库操作时，数据库选择在创建仓储对象时完成，后续方法内部不再重复处理库名。

```go
type AuditService struct {
    users *UserRepo
    logs  *AccessLogRepo
}

func NewAuditService(db *sqlx.Manager) (*AuditService, error) {
    users, logs, err := NewRepos(db)
    if err != nil {
        return nil, err
    }
    return &AuditService{users: users, logs: logs}, nil
}
```

## 租户数据库

如果每个租户一个数据库，租户名也只是 `GetEngine` 的参数。

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

## 高频查询

热点路径可以从 Engine 准备语句。普通查询和预编译查询仍然都从同一个 Engine 出发。

```go
type PreparedUserRepo struct {
    e          *sqlx.Engine
    findActive *sqlx.DynamicStmt
}

func NewPreparedUserRepo(e *sqlx.Engine) (*PreparedUserRepo, error) {
    stmt, err := e.PrepareNamed(`
        SELECT id, name, email, age, status, created_at, updated_at
        FROM users
        WHERE status = :status
        ORDER BY id DESC
        LIMIT :limit
    `)
    if err != nil {
        return nil, err
    }

    return &PreparedUserRepo{
        e:          e,
        findActive: stmt,
    }, nil
}

func (r *PreparedUserRepo) FindActive(limit int) ([]User, error) {
    var users []User
    err := r.findActive.Select(&users, map[string]any{
        "status": "active",
        "limit":  limit,
    })
    return users, err
}
```

写入热点也使用 Engine 的预编译命名语句。

```go
type FastUserRepo struct {
    insertUser *sqlx.NamedStmt
}

func NewFastUserRepo(e *sqlx.Engine) (*FastUserRepo, error) {
    insertUser, err := e.PrepareNamed(`
        INSERT INTO users (name, email, age, status)
        VALUES (:name, :email, :age, :status)
        RETURNING id
    `)
    if err != nil {
        return nil, err
    }

    return &FastUserRepo{insertUser: insertUser}, nil
}

func (r *FastUserRepo) Create(user User) (int64, error) {
    var id int64
    err := r.insertUser.Get(&id, user)
    return id, err
}
```

## 迁移切换

迁移时，仓储层继续拿 `GetEngine("app")`。配置层决定底层连接是 SQLite 还是 PostgreSQL。

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

SQLite 配置示例。

```go
configs := []StoreConfig{
    {
        Name:   "app",
        Driver: "sqlite3",
        DSN:    "file:app.db?_foreign_keys=on",
    },
    {
        Name:   "logs",
        Driver: "sqlite3",
        DSN:    "file:logs.db?_foreign_keys=on",
    },
}
```

PostgreSQL 配置示例。

```go
configs := []StoreConfig{
    {
        Name:   "app",
        Driver: "postgres",
        DSN:    "postgres://user:pass@localhost/app?sslmode=disable",
    },
    {
        Name:   "logs",
        Driver: "postgres",
        DSN:    "postgres://user:pass@localhost/logs?sslmode=disable",
    },
}
```

## 推荐的调用选择

拿数据库执行入口时，只使用 `GetEngine`。

```go
app, err := db.GetEngine("app")
```

简单一两个参数，使用 `GetP`、`SelectP`、`ExecP`。

```go
user, err := repo.FindByID(1001)
```

参数较多，使用 `GetNamed`、`SelectNamed`、`ExecNamed`。

```go
err := repo.CreateByNamed(User{
    Name:   "Tom",
    Email:  "tom@example.com",
    Age:    20,
    Status: "active",
})
```

条件可选、存在 `IN`、分页筛选组合较多，使用 Engine 的动态 SQL。

```go
users, err := repo.Search(UserQuery{
    Name:   "%tom%",
    Status: "active",
    IDs:    []int64{1, 2, 3},
})
```

标准单表增删改查，直接使用 Engine 的 `ExecNamed`、`GetNamed`、`SelectNamed`、`ExecP`。

```go
err := repo.Create(User{
    Name:   "Tom",
    Email:  "tom@example.com",
    Age:    20,
    Status: "active",
})
```

多步写入需要原子性，使用 Engine 的事务 API。

```go
err := service.Transfer(1, 2, 100)
```

热点路径或批量循环，使用 Engine 的预编译 API。

```go
users, err := preparedRepo.FindActive(100)
```

## 不推荐的调用方式

不推荐在业务代码里绕过 Engine 直接调用底层 `DB`。

```go
err := rawDB.Get(&user, "SELECT * FROM users WHERE id = $1", id)
```

原始调用里写死 PostgreSQL 占位符，会让代码偏向 PostgreSQL。

```go
err := rawDB.Get(&user, "SELECT * FROM users WHERE id = ?", id)
```

原始调用里直接写 `?`，在 PostgreSQL 下不会自动转换。应从 `GetEngine("app")` 拿到 Engine，再调用 `GetP`。

```go
query := "SELECT * FROM users WHERE 1=1"
if status != "" {
    query += " AND status = '" + status + "'"
}
```

手动拼接条件不利于迁移，也容易引入安全问题。应使用 Engine 的动态条件块。

## 最小完整示例

下面是一个应用启动后创建仓储对象，并执行查询和事务的完整调用形态。

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
