# sqlx 使用指南

本文档覆盖 sqlx 的典型使用场景：DB 初始化、基础 CRUD、以及 `db.LazyEngine()` 动态 SQL。

## 目录

- [sqlx 使用指南](#sqlx-使用指南)
  - [目录](#目录)
  - [初始化冲突点2](#初始化冲突点2)
    - [单数据库](#单数据库)
    - [多数据库 (Manager)](#多数据库-manager)
  - [DB 基础 CRUD](#db-基础-crud)
    - [模型定义](#模型定义)
    - [查询多条 (Select)](#查询多条-select)
    - [查询单条 (Get)](#查询单条-get)
    - [写入 (Exec / MustExec)](#写入-exec--mustexec)
    - [命名参数 (NamedExec / NamedQuery)](#命名参数-namedexec--namedquery)
    - [批量插入](#批量插入)
    - [IN 子句](#in-子句)
    - [事务 (WithTransaction)](#事务-withtransaction)
  - [DB.Engine 动态 SQL](#dbengine-动态-sql)
    - [获取 Engine](#获取-engine)
    - [多条件动态查询](#多条件动态查询)
    - [IN 子句 + 动态裁剪](#in-子句--动态裁剪)
    - [分页查询](#分页查询)
    - [关联查询](#关联查询)
    - [结构体参数](#结构体参数)
    - [预处理语句 (PrepareNamed)](#预处理语句-preparenamed)
    - [条件判断规则](#条件判断规则)
  - [完整示例 (Repository 模式)](#完整示例-repository-模式)

---

## 初始化冲突点2

### 单数据库

```go
import (
    _ "github.com/lib/pq"
    "github.com/tfnick/sqlx"
)

// 方式1：Open（不自动 Ping）
db, err := sqlx.Open("postgres", "postgres://user:pass@localhost/db?sslmode=disable")
if err != nil {
    log.Fatal(err)
}
db.Ping()

// 方式2：MustOpen（失败 panic）
db := sqlx.MustOpen("mysql", "user:pass@tcp(localhost:3306)/db?parseTime=true")

// 方式3：Connect（自动 Ping，常用）
db, err := sqlx.Connect("sqlite3", "file:test.db")
defer db.Close()
```

### 多数据库 (Manager)

`*Manager` 只负责 DB 连接的注册和获取。拿到 `*DB` 后所有操作方式与单数据库完全一致。

```go
mgr := sqlx.NewManager()
mgr.MustOpen("app",  "postgres", "postgres://.../appdb")
mgr.MustOpen("logs", "postgres", "postgres://.../logsdb")
defer mgr.Close()

// 获取 DB —— 后续全部操作都在 DB 上进行
appDB  := mgr.MustDB("app")
logsDB := mgr.MustDB("logs")

// 使用方式与单 DB 完全一致
appDB.Select(&users, "SELECT * FROM users")
logsDB.Exec("INSERT INTO access_logs (action) VALUES ($1)", "login")

// 空 name 默认为 "app"
defaultDB := mgr.MustDB("")
```

---

## DB 基础 CRUD

以下示例使用 PostgreSQL 语法（`$1, $2`）。MySQL/SQLite3 自动适配 `?`。

### 模型定义

```go
type User struct {
    ID        int64     `db:"id"`
    Name      string    `db:"name"`
    Email     string    `db:"email"`
    Age       int       `db:"age"`
    Status    string    `db:"status"`
    CreatedAt time.Time `db:"created_at"`
}
```

### 查询多条 (Select)

```go
// 条件查询
var users []User
db.Select(&users, "SELECT * FROM users WHERE status=$1", "active")

// 全表查询
db.Select(&users, "SELECT * FROM users ORDER BY id")

// 扫描简单类型（count、string）
var names []string
db.Select(&names, "SELECT name FROM users")

var count int
db.Get(&count, "SELECT count(*) FROM users")
```

### 查询单条 (Get)

```go
var user User
err := db.Get(&user, "SELECT * FROM users WHERE id=$1", 1)
if err == sql.ErrNoRows {
    // 未找到
}
```

### 写入 (Exec / MustExec)

```go
// 返回 error
result, err := db.Exec(
    "UPDATE users SET name=$1 WHERE id=$2", "新名字", 1,
)
affected, _ := result.RowsAffected()

// 失败 panic（适合初始化脚本）
db.MustExec("INSERT INTO users (name, email) VALUES ($1, $2)", "张三", "zs@test.com")
```

### 命名参数 (NamedExec / NamedQuery)

用 `:name` 占位符替代 `$1, $2`，参数通过 struct 或 map 传入：

```go
// 使用 struct
user := User{Name: "张三", Email: "zs@test.com"}
db.NamedExec("INSERT INTO users (name, email) VALUES (:name, :email)", &user)

// 使用 map
db.NamedExec(
    "INSERT INTO users (name, email) VALUES (:name, :email)",
    map[string]interface{}{"name": "李四", "email": "ls@test.com"},
)

// NamedQuery 查询
rows, err := db.NamedQuery(
    "SELECT * FROM users WHERE name=:name",
    map[string]interface{}{"name": "张三"},
)
```

### 批量插入

传入 struct 切片或 map 切片，一次性插入多行：

```go
// struct 切片
users := []User{
    {Name: "张三", Email: "zs@test.com"},
    {Name: "李四", Email: "ls@test.com"},
}
db.NamedExec("INSERT INTO users (name, email) VALUES (:name, :email)", users)

// map 切片
userMaps := []map[string]interface{}{
    {"name": "张三", "email": "zs@test.com"},
    {"name": "李四", "email": "ls@test.com"},
}
db.NamedExec("INSERT INTO users (name, email) VALUES (:name, :email)", userMaps)
```

### IN 子句

使用 `sqlx.In()` 展开切片，配合 `db.Rebind()` 适配数据库占位符：

```go
ids := []int64{1, 2, 3, 4, 5}
query, args, _ := sqlx.In("SELECT * FROM users WHERE id IN (?)", ids)
// query = "SELECT * FROM users WHERE id IN (?,?,?,?,?)"
// args  = [1, 2, 3, 4, 5]

query = db.Rebind(query)
// PostgreSQL: "SELECT * FROM users WHERE id IN ($1,$2,$3,$4,$5)"
// MySQL:      "SELECT * FROM users WHERE id IN (?,?,?,?,?)"

var users []User
db.Select(&users, query, args...)
```

### 事务 (WithTransaction)

`db.WithTransaction` 自动管理 commit/rollback：函数返回 nil 自动提交，返回 error 或 panic 自动回滚。

```go
func transfer(db *sqlx.DB, fromID, toID int64, amount float64) error {
    return db.WithTransaction(func(tx *sqlx.Tx) error {
        // 扣减
        _, err := tx.Exec(
            "UPDATE account SET balance=balance-$1 WHERE id=$2",
            amount, fromID,
        )
        if err != nil {
            return err
        }
        // 增加
        _, err = tx.Exec(
            "UPDATE account SET balance=balance+$1 WHERE id=$2",
            amount, toID,
        )
        return err
    })
}

// 多数据源事务
func transferByManager(mgr *sqlx.Manager, fromID, toID int64, amount float64) error {
    db := mgr.MustDB("app")
    return db.WithTransaction(func(tx *sqlx.Tx) error { ... })
}
```

---

## DB.Engine 动态 SQL

当查询条件不确定、需要根据参数动态裁剪 SQL 时，通过 `db.LazyEngine()` 获取 Engine。**Engine 的 API 与 DB 完全一致**，只是内部多了 `#[ ]` 条件块的处理。

### 获取 Engine

```go
// 推荐：LazyEngine（单例、懒加载）
engine := db.LazyEngine()

// 多次调用返回同一个实例
e1 := db.LazyEngine()
e2 := db.LazyEngine()
// e1 == e2  → true
```

### 多条件动态查询

`#[ ... ]` 块内的条件，仅在对应参数为"有效值"时保留；参数为空值（`""` / `0` / `nil` / `false` / 空切片）时整块被移除。

```go
func SearchUsers(db *sqlx.DB, name string, minAge int, status string) ([]User, error) {
    sql := `
    SELECT id, name, email, age, status, created_at
    FROM users
    WHERE 1=1
    #[ AND name LIKE :name ]
    #[ AND age >= :min_age ]
    #[ AND status = :status ]
    ORDER BY created_at DESC
    `

    params := map[string]interface{}{
        "name":    name,
        "min_age": minAge,
        "status":  status,
    }

    var users []User
    err := db.LazyEngine().Select(&users, sql, params)
    return users, err
}

// 使用示例
func main() {
    db, _ := sqlx.Open("postgres", "...")

    // 场景1：只按名称查
    SearchUsers(db, "张%", 0, "")
    // → SQL: SELECT ... WHERE 1=1 AND name LIKE $1 ORDER BY ...

    // 场景2：名称 + 年龄
    SearchUsers(db, "张%", 18, "")
    // → SQL: SELECT ... WHERE 1=1 AND name LIKE $1 AND age >= $2 ORDER BY ...

    // 场景3：全部条件
    SearchUsers(db, "张%", 18, "active")
    // → SQL: SELECT ... WHERE 1=1 AND name LIKE $1 AND age >= $2 AND status = $3 ORDER BY ...

    // 场景4：无条件（空值全部移除）
    SearchUsers(db, "", 0, "")
    // → SQL: SELECT ... WHERE 1=1 ORDER BY ...
}
```

> **Tips**: 始终使用 `WHERE 1=1` 开头，避免"第一个条件要不要加 AND"的问题。

### IN 子句 + 动态裁剪

空切片自动移除整个 `#[ AND id IN :ids ]` 块：

```go
func SearchByIDs(db *sqlx.DB, ids []int64, statuses []string) ([]User, error) {
    sql := `
    SELECT id, name, email, age, status
    FROM users
    WHERE 1=1
    #[ AND id IN :ids ]
    #[ AND status IN :statuses ]
    ORDER BY id
    `

    params := map[string]interface{}{
        "ids":      ids,
        "statuses": statuses,
    }

    var users []User
    err := db.LazyEngine().Select(&users, sql, params)
    return users, err
}

// 场景
SearchByIDs(db, []int64{1, 2, 3}, nil)
// → SQL: ... WHERE 1=1 AND id IN ($1,$2,$3) ORDER BY id

SearchByIDs(db, []int64{}, []string{"active"})
// → SQL: ... WHERE 1=1 AND status IN ($1) ORDER BY id
```

### 分页查询

```go
func SearchWithPaging(db *sqlx.DB, name, status string, page, pageSize int) ([]User, int, error) {
    params := map[string]interface{}{
        "name":   name,
        "status": status,
    }

    // 查询列表
    listSQL := `
    SELECT id, name, email, age, status
    FROM users
    WHERE 1=1
    #[ AND name LIKE :name ]
    #[ AND status = :status ]
    ORDER BY id DESC
    LIMIT :limit OFFSET :offset
    `
    listParams := map[string]interface{}{
        "name":   name,
        "status": status,
        "limit":  pageSize,
        "offset": (page - 1) * pageSize,
    }

    var users []User
    if err := db.LazyEngine().Select(&users, listSQL, listParams); err != nil {
        return nil, 0, err
    }

    // 查询总数
    countSQL := `
    SELECT count(*)
    FROM users
    WHERE 1=1
    #[ AND name LIKE :name ]
    #[ AND status = :status ]
    `

    var total int
    if err := db.LazyEngine().Get(&total, countSQL, params); err != nil {
        return nil, 0, err
    }

    return users, total, nil
}
```

### 关联查询

```go
type UserWithDept struct {
    ID       int64  `db:"id"`
    Name     string `db:"name"`
    DeptName string `db:"dept_name"`
}

func SearchWithDept(db *sqlx.DB, userName, deptName string) ([]UserWithDept, error) {
    sql := `
    SELECT u.id, u.name, d.name AS dept_name
    FROM users u
    LEFT JOIN departments d ON u.dept_id = d.id
    WHERE 1=1
    #[ AND u.name LIKE :user_name ]
    #[ AND d.name LIKE :dept_name ]
    ORDER BY u.id
    `

    var results []UserWithDept
    err := db.LazyEngine().Select(&results, sql, map[string]interface{}{
        "user_name": userName,
        "dept_name": deptName,
    })
    return results, err
}
```

### 结构体参数

Engine 的 `arg` 参数既可以是 `map[string]interface{}`，也可以是带 `db` tag 的结构体：

```go
type UserQuery struct {
    Name   string  `db:"name"`
    Status string  `db:"status"`
    MinAge int     `db:"min_age"`
    MaxAge int     `db:"max_age"`
    IDs    []int64 `db:"ids"`
}

func SearchByStruct(db *sqlx.DB, q UserQuery) ([]User, error) {
    sql := `
    SELECT id, name, email, age, status
    FROM users
    WHERE 1=1
    #[ AND name LIKE :name ]
    #[ AND status = :status ]
    #[ AND age >= :min_age ]
    #[ AND age <= :max_age ]
    #[ AND id IN :ids ]
    ORDER BY id DESC
    `

    var users []User
    err := db.LazyEngine().Select(&users, sql, q)
    return users, err
}

// 使用
users, _ := SearchByStruct(db, UserQuery{
    Name:   "张%",
    MinAge: 18,
    Status: "active",
})
```

> **区分零值与未设置**：使用指针类型。`*int` 为 nil 时条件移除，`&0` 时条件保留。
>
> ```go
> type Query struct {
>     Age *int `db:"age"`   // nil = 不筛选，&0 = 查 age=0
> }
> ```

### 预处理语句 (PrepareNamed)

高频执行的动态 SQL 可用 `PrepareNamed` 预编译：

```go
type UserRepo struct {
    db       *sqlx.DB
    findStmt *sqlx.DynNamedStmt
}

func NewUserRepo(db *sqlx.DB) (*UserRepo, error) {
    repo := &UserRepo{db: db}

    stmt, err := db.LazyEngine().PrepareNamed(`
        SELECT id, name, email, age, status
        FROM users
        WHERE 1=1
        #[ AND name LIKE :name ]
        #[ AND status = :status ]
        ORDER BY id
    `)
    if err != nil {
        return nil, err
    }
    repo.findStmt = stmt
    return repo, nil
}

func (r *UserRepo) Find(name, status string) ([]User, error) {
    var users []User
    err := r.findStmt.Select(&users, map[string]interface{}{
        "name":   name,
        "status": status,
    })
    return users, err
}
```

### 条件判断规则

| 参数值 | 是否保留 | 说明 |
|---|---|---|
| `nil` | 移除 | 参数不存在或为 nil |
| `""` | 移除 | 空字符串 |
| 非空字符串 | 保留 | |
| `0` / `0.0` | 移除 | 零值 |
| 非零数值 | 保留 | |
| `false` | 移除 | 布尔 false |
| `true` | 保留 | 布尔 true |
| `[]int{}` | 移除 | 空切片 |
| 非空切片 | 保留 | |
| nil 指针 | 移除 | |
| 非 nil 指针 | **保留** | 即使指向零值（如 `&0`、`&""`） |

---

## 完整示例 (Repository 模式)

```go
package main

import (
    "fmt"
    "log"
    "time"

    "github.com/tfnick/sqlx"
)

type User struct {
    ID        int64     `db:"id"`
    Name      string    `db:"name"`
    Email     string    `db:"email"`
    Age       int       `db:"age"`
    Status    string    `db:"status"`
    CreatedAt time.Time `db:"created_at"`
}

type UserQuery struct {
    Name   string  `db:"name"`
    Status string  `db:"status"`
    MinAge int     `db:"min_age"`
    MaxAge int     `db:"max_age"`
    IDs    []int64 `db:"ids"`
}

// Repository 只持有 *DB，所有操作统一入口
type UserRepo struct {
    db *sqlx.DB
}

func NewUserRepo(db *sqlx.DB) *UserRepo {
    return &UserRepo{db: db}
}

// 动态查询 — 内部用 db.LazyEngine()
func (r *UserRepo) Find(q UserQuery) ([]User, error) {
    sql := `
    SELECT id, name, email, age, status, created_at
    FROM users
    WHERE 1=1
    #[ AND name LIKE :name ]
    #[ AND status = :status ]
    #[ AND age >= :min_age ]
    #[ AND age <= :max_age ]
    #[ AND id IN :ids ]
    ORDER BY created_at DESC
    `
    var users []User
    return users, r.db.LazyEngine().Select(&users, sql, q)
}

// 简单查询 — 直接使用 DB
func (r *UserRepo) FindAll() ([]User, error) {
    var users []User
    return users, r.db.Select(&users, "SELECT * FROM users ORDER BY id")
}

// 创建 — Engine 处理命名参数
func (r *UserRepo) Create(user *User) (int64, error) {
    sql := `
    INSERT INTO users (name, email, age, status, created_at)
    VALUES (:name, :email, :age, :status, NOW())
    RETURNING id
    `
    var id int64
    return id, r.db.LazyEngine().Get(&id, sql, user)
}

// 更新 — Engine 处理命名参数
func (r *UserRepo) Update(user *User) error {
    sql := `
    UPDATE users SET
        name = :name, email = :email, age = :age,
        status = :status, updated_at = NOW()
    WHERE id = :id
    `
    _, err := r.db.LazyEngine().Exec(sql, user)
    return err
}

// 删除 — DB 足够
func (r *UserRepo) Delete(id int64) error {
    _, err := r.db.Exec("DELETE FROM users WHERE id=$1", id)
    return err
}

// 事务
func (r *UserRepo) Transfer(fromID, toID int64, amount float64) error {
    return r.db.WithTransaction(func(tx *sqlx.Tx) error {
        tx.Exec("UPDATE account SET balance=balance-$1 WHERE id=$2", amount, fromID)
        tx.Exec("UPDATE account SET balance=balance+$1 WHERE id=$2", amount, toID)
        return nil
    })
}

func main() {
    db, _ := sqlx.Open("postgres", "postgres://...")
    defer db.Close()

    repo := NewUserRepo(db)

    // 动态组合查询
    users, _ := repo.Find(UserQuery{
        Name:   "张%",
        Status: "active",
        MinAge: 18,
        IDs:    []int64{1, 2, 3, 4, 5},
    })
    fmt.Printf("Found %d users\n", len(users))
}
```
