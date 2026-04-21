# sqlx Engine 动态SQL使用指南

本文档展示如何使用 `sqlx.Engine` 实现类似 sqltoy 的动态SQL能力，覆盖常见CRUD场景。

## 目录

- [快速开始](#快速开始)
- [初始化](#初始化)
- [查询操作 (Read)](#查询操作-read)
  - [单条件查询](#单条件查询)
  - [多条件动态查询](#多条件动态查询)
  - [IN 子句查询](#in-子句查询)
  - [分页查询](#分页查询)
  - [关联查询](#关联查询)
  - [查询单条记录](#查询单条记录)
- [插入操作 (Create)](#插入操作-create)
  - [单条插入](#单条插入)
  - [批量插入](#批量插入)
  - [动态字段插入](#动态字段插入)
- [更新操作 (Update)](#更新操作-update)
  - [条件更新](#条件更新)
  - [动态字段更新](#动态字段更新)
- [删除操作 (Delete)](#删除操作-delete)
  - [条件删除](#条件删除)
- [事务支持](#事务支持)
- [预处理语句](#预处理语句)
- [参数类型支持](#参数类型支持)
- [条件判断规则](#条件判断规则)

---

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/tfnick/sqlx"
)

type User struct {
    ID        int64  `db:"id"`
    Name      string `db:"name"`
    Email     string `db:"email"`
    Age       int    `db:"age"`
    Status    string `db:"status"`
    CreatedAt string `db:"created_at"`
}

func main() {
    // 连接数据库
    db, err := sqlx.Open("postgres", "postgres://user:pass@localhost/db?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // 创建 Engine
    engine := sqlx.NewEngine(db)

    // 动态查询示例
    users, err := findUsers(context.Background(), engine, "张", 18, nil)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Found %d users\n", len(users))
}
```

---

## 初始化

```go
import (
    "github.com/tfnick/sqlx"
)

// 方式1：从已有 sqlx.DB 创建
db, _ := sqlx.Open("postgres", dsn)
engine := sqlx.NewEngine(db)

// 方式2：直接使用 MustOpen
db := sqlx.MustOpen("mysql", dsn)
engine := sqlx.NewEngine(db)

// 获取底层 DB（如需要）
underlyingDB := engine.DB()
```

---

## 查询操作 (Read)

### 单条件查询

```go
// 简单查询 - 根据ID查询
func getUserByID(ctx context.Context, engine *sqlx.Engine, id int64) (*User, error) {
    sql := `SELECT id, name, email, age, status, created_at 
            FROM user 
            WHERE id = :id`
    
    var user User
    err := engine.Get(ctx, &user, sql, map[string]interface{}{
        "id": id,
    })
    if err != nil {
        return nil, err
    }
    return &user, nil
}
```

### 多条件动态查询

这是动态SQL的核心能力，使用 `#[ ]` 语法实现条件裁剪：

```go
// 动态条件查询
// #[ ] 块内的条件仅在参数有值时才保留
func findUsers(ctx context.Context, engine *sqlx.Engine, 
    name string, minAge int, status string) ([]User, error) {
    
    sql := `
    SELECT id, name, email, age, status, created_at 
    FROM user 
    WHERE 1=1
    #[ AND name LIKE :name ]
    #[ AND age >= :min_age ]
    #[ AND age <= :max_age ]
    #[ AND status = :status ]
    #[ AND email LIKE :email ]
    ORDER BY created_at DESC
    `
    
    params := map[string]interface{}{
        "name":    name,      // 如果不为空，条件生效
        "min_age": minAge,    // 如果不为0，条件生效
        "status":  status,    // 如果不为空，条件生效
    }
    
    var users []User
    err := engine.Select(ctx, &users, sql, params)
    return users, err
}

// 使用示例
func main() {
    // 场景1：只按名称模糊查询
    users, _ := findUsers(ctx, engine, "张%", 0, "")
    // 生成SQL: SELECT ... WHERE 1=1 AND name LIKE $1 ORDER BY ...
    
    // 场景2：按名称和年龄查询
    users, _ := findUsers(ctx, engine, "张%", 18, "")
    // 生成SQL: SELECT ... WHERE 1=1 AND name LIKE $1 AND age >= $2 ORDER BY ...
    
    // 场景3：全部条件
    users, _ := findUsers(ctx, engine, "张%", 18, "active")
    // 生成SQL: SELECT ... WHERE 1=1 AND name LIKE $1 AND age >= $2 AND status = $3 ORDER BY ...
    
    // 场景4：无条件（查询全部）
    users, _ := findUsers(ctx, engine, "", 0, "")
    // 生成SQL: SELECT ... WHERE 1=1 ORDER BY ...
}
```

### IN 子句查询

IN 子句是动态SQL的典型应用场景：

```go
// IN 子句查询 - 空切片自动移除条件
func findUsersByIDs(ctx context.Context, engine *sqlx.Engine, 
    ids []int64, statuses []string) ([]User, error) {
    
    sql := `
    SELECT id, name, email, age, status, created_at 
    FROM user 
    WHERE 1=1
    #[ AND id IN :ids ]
    #[ AND status IN :statuses ]
    ORDER BY id
    `
    
    params := map[string]interface{}{
        "ids":       ids,       // 空切片 → 条件移除
        "statuses":  statuses,  // 空切片 → 条件移除
    }
    
    var users []User
    err := engine.Select(ctx, &users, sql, params)
    return users, err
}

// 使用示例
func main() {
    // 场景1：指定ID列表
    users, _ := findUsersByIDs(ctx, engine, []int64{1, 2, 3}, nil)
    // 生成SQL: SELECT ... WHERE 1=1 AND id IN ($1, $2, $3) ORDER BY id
    
    // 场景2：指定状态列表
    users, _ := findUsersByIDs(ctx, engine, nil, []string{"active", "pending"})
    // 生成SQL: SELECT ... WHERE 1=1 AND status IN ($1, $2) ORDER BY id
    
    // 场景3：两个条件都有
    users, _ := findUsersByIDs(ctx, engine, []int64{1, 2, 3}, []string{"active"})
    // 生成SQL: SELECT ... WHERE 1=1 AND id IN ($1, $2, $3) AND status IN ($4) ORDER BY id
    
    // 场景4：两个都是空切片（查询全部）
    users, _ := findUsersByIDs(ctx, engine, []int64{}, []string{})
    // 生成SQL: SELECT ... WHERE 1=1 ORDER BY id
}
```

### 分页查询

```go
// 分页查询
func findUsersWithPaging(ctx context.Context, engine *sqlx.Engine,
    name string, status string, page, pageSize int) ([]User, int, error) {
    
    // 查询列表
    listSQL := `
    SELECT id, name, email, age, status, created_at 
    FROM user 
    WHERE 1=1
    #[ AND name LIKE :name ]
    #[ AND status = :status ]
    ORDER BY created_at DESC
    LIMIT :limit OFFSET :offset
    `
    
    // 查询总数
    countSQL := `
    SELECT COUNT(*) as total
    FROM user 
    WHERE 1=1
    #[ AND name LIKE :name ]
    #[ AND status = :status ]
    `
    
    params := map[string]interface{}{
        "name":    name,
        "status":  status,
        "limit":   pageSize,
        "offset":  (page - 1) * pageSize,
    }
    
    var users []User
    if err := engine.Select(ctx, &users, listSQL, params); err != nil {
        return nil, 0, err
    }
    
    var total int
    if err := engine.Get(ctx, &total, countSQL, params); err != nil {
        return nil, 0, err
    }
    
    return users, total, nil
}

// 使用示例
func main() {
    // 第1页，每页10条
    users, total, _ := findUsersWithPaging(ctx, engine, "张%", "active", 1, 10)
    fmt.Printf("Total: %d, Page 1: %d users\n", total, len(users))
}
```

### 关联查询

```go
type UserWithDept struct {
    ID           int64  `db:"id"`
    Name         string `db:"name"`
    Email        string `db:"email"`
    DepartmentID int64  `db:"department_id"`
    DeptName     string `db:"dept_name"`
}

// 多表关联查询
func findUsersWithDept(ctx context.Context, engine *sqlx.Engine,
    userName string, deptName string) ([]UserWithDept, error) {
    
    sql := `
    SELECT u.id, u.name, u.email, u.department_id, d.name as dept_name
    FROM user u
    LEFT JOIN department d ON u.department_id = d.id
    WHERE 1=1
    #[ AND u.name LIKE :user_name ]
    #[ AND d.name LIKE :dept_name ]
    #[ AND u.status = :status ]
    ORDER BY u.id
    `
    
    params := map[string]interface{}{
        "user_name": userName,
        "dept_name": deptName,
    }
    
    var results []UserWithDept
    err := engine.Select(ctx, &results, sql, params)
    return results, err
}
```

### 查询单条记录

```go
// 使用 Get 查询单条
func getUserByEmail(ctx context.Context, engine *sqlx.Engine, email string) (*User, error) {
    sql := `
    SELECT id, name, email, age, status, created_at 
    FROM user 
    WHERE email = :email
    LIMIT 1
    `
    
    var user User
    err := engine.Get(ctx, &user, sql, map[string]interface{}{
        "email": email,
    })
    if err == sql.ErrNoRows {
        return nil, nil // 未找到
    }
    if err != nil {
        return nil, err
    }
    return &user, nil
}
```

---

## 插入操作 (Create)

### 单条插入

```go
// 单条插入
func createUser(ctx context.Context, engine *sqlx.Engine, user *User) (int64, error) {
    sql := `
    INSERT INTO user (name, email, age, status, created_at)
    VALUES (:name, :email, :age, :status, NOW())
    RETURNING id
    `
    
    params := map[string]interface{}{
        "name":   user.Name,
        "email":  user.Email,
        "age":    user.Age,
        "status": user.Status,
    }
    
    var id int64
    err := engine.Get(ctx, &id, sql, params)
    return id, err
}

// MySQL 版本（使用 LastInsertId）
func createUserMySQL(ctx context.Context, engine *sqlx.Engine, user *User) (int64, error) {
    sql := `
    INSERT INTO user (name, email, age, status, created_at)
    VALUES (:name, :email, :age, :status, NOW())
    `
    
    result, err := engine.Exec(ctx, sql, map[string]interface{}{
        "name":   user.Name,
        "email":  user.Email,
        "age":    user.Age,
        "status": user.Status,
    })
    if err != nil {
        return 0, err
    }
    return result.LastInsertId()
}
```

### 批量插入

```go
// 批量插入 - 使用结构体切片
func batchCreateUsers(ctx context.Context, engine *sqlx.Engine, users []*User) error {
    sql := `
    INSERT INTO user (name, email, age, status, created_at)
    VALUES (:name, :email, :age, :status, NOW())
    `
    
    // 转换为切片
    var userSlice []User
    for _, u := range users {
        userSlice = append(userSlice, *u)
    }
    
    _, err := engine.Exec(ctx, sql, userSlice)
    return err
}
```

### 动态字段插入

```go
// 动态字段插入 - 只插入有值的字段
func createUserDynamic(ctx context.Context, engine *sqlx.Engine, 
    name string, email string, age int) (int64, error) {
    
    sql := `
    INSERT INTO user (
        name
        #[ , email ]
        #[ , age ]
        #[ , status ]
    ) VALUES (
        :name
        #[ , :email ]
        #[ , :age ]
        #[ , :status ]
    )
    RETURNING id
    `
    
    params := map[string]interface{}{
        "name":   name,
        "email":  email,  // 空字符串 → 不插入
        "age":    age,    // 0 → 不插入
        "status": "active", // 有值 → 插入
    }
    
    var id int64
    err := engine.Get(ctx, &id, sql, params)
    return id, err
}
```

---

## 更新操作 (Update)

### 条件更新

```go
// 简单更新
func updateUserStatus(ctx context.Context, engine *sqlx.Engine, 
    userID int64, newStatus string) error {
    
    sql := `
    UPDATE user 
    SET status = :status, updated_at = NOW()
    WHERE id = :id
    `
    
    _, err := engine.Exec(ctx, sql, map[string]interface{}{
        "id":     userID,
        "status": newStatus,
    })
    return err
}
```

### 动态字段更新

```go
// 动态更新 - 只更新传入的字段
func updateUserDynamic(ctx context.Context, engine *sqlx.Engine, 
    userID int64, updates map[string]interface{}) error {
    
    sql := `
    UPDATE user SET
        updated_at = NOW()
        #[ , name = :name ]
        #[ , email = :email ]
        #[ , age = :age ]
        #[ , status = :status ]
    WHERE id = :id
    `
    
    params := map[string]interface{}{
        "id": userID,
    }
    
    // 只添加有值的字段
    for k, v := range updates {
        params[k] = v
    }
    
    _, err := engine.Exec(ctx, sql, params)
    return err
}

// 使用示例
func main() {
    // 只更新名称
    updateUserDynamic(ctx, engine, 1, map[string]interface{}{
        "name": "新名称",
    })
    // 生成SQL: UPDATE user SET updated_at = NOW() , name = $1 WHERE id = $2
    
    // 更新多个字段
    updateUserDynamic(ctx, engine, 1, map[string]interface{}{
        "name":  "新名称",
        "email": "new@email.com",
        "status": "inactive",
    })
    // 生成SQL: UPDATE user SET updated_at = NOW() , name = $1 , email = $2 , status = $3 WHERE id = $4
}
```

---

## 删除操作 (Delete)

### 条件删除

```go
// 单条删除
func deleteUser(ctx context.Context, engine *sqlx.Engine, userID int64) error {
    sql := `DELETE FROM user WHERE id = :id`
    _, err := engine.Exec(ctx, sql, map[string]interface{}{
        "id": userID,
    })
    return err
}

// 批量删除
func deleteUsersByIDs(ctx context.Context, engine *sqlx.Engine, ids []int64) error {
    sql := `DELETE FROM user WHERE id IN :ids`
    _, err := engine.Exec(ctx, sql, map[string]interface{}{
        "ids": ids,
    })
    return err
}

// 条件删除
func deleteUsersByCondition(ctx context.Context, engine *sqlx.Engine,
    status string, beforeDate string) error {
    
    sql := `
    DELETE FROM user 
    WHERE 1=1
    #[ AND status = :status ]
    #[ AND created_at < :before_date ]
    `
    
    params := map[string]interface{}{
        "status":      status,
        "before_date": beforeDate,
    }
    
    _, err := engine.Exec(ctx, sql, params)
    return err
}
```

---

## 事务支持

```go
// 事务操作
func transferBalance(ctx context.Context, engine *sqlx.Engine,
    fromUserID, toUserID int64, amount float64) error {
    
    // 开启事务
    tx, err := engine.DB().BeginTxx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()
    
    // 创建事务内的 Engine
    txEngine := sqlx.NewEngine(tx.(*sqlx.Tx))
    
    // 扣减余额
    deductSQL := `
        UPDATE account SET balance = balance - :amount 
        WHERE user_id = :user_id AND balance >= :amount
    `
    result, err := txEngine.Exec(ctx, deductSQL, map[string]interface{}{
        "amount":  amount,
        "user_id": fromUserID,
    })
    if err != nil {
        return err
    }
    
    affected, _ := result.RowsAffected()
    if affected == 0 {
        return fmt.Errorf("余额不足")
    }
    
    // 增加余额
    addSQL := `
        UPDATE account SET balance = balance + :amount 
        WHERE user_id = :user_id
    `
    _, err = txEngine.Exec(ctx, addSQL, map[string]interface{}{
        "amount":  amount,
        "user_id": toUserID,
    })
    if err != nil {
        return err
    }
    
    // 记录日志
    logSQL := `
        INSERT INTO transfer_log (from_user_id, to_user_id, amount, created_at)
        VALUES (:from_user_id, :to_user_id, :amount, NOW())
    `
    _, err = txEngine.Exec(ctx, logSQL, map[string]interface{}{
        "from_user_id": fromUserID,
        "to_user_id":   toUserID,
        "amount":       amount,
    })
    if err != nil {
        return err
    }
    
    return tx.Commit()
}
```

---

## 预处理语句

对于高频执行的SQL，使用预处理语句提升性能：

```go
// 预处理语句
type UserRepository struct {
    engine      *sqlx.Engine
    findStmt    *sqlx.DynNamedStmt
    getByIDStmt *sqlx.DynNamedStmt
}

func NewUserRepository(engine *sqlx.Engine) (*UserRepository, error) {
    repo := &UserRepository{engine: engine}
    
    var err error
    
    // 预编译查询语句
    repo.findStmt, err = engine.PrepareNamed(`
        SELECT id, name, email, age, status, created_at 
        FROM user 
        WHERE 1=1
        #[ AND name LIKE :name ]
        #[ AND status = :status ]
        ORDER BY id
    `)
    if err != nil {
        return nil, err
    }
    
    repo.getByIDStmt, err = engine.PrepareNamed(`
        SELECT id, name, email, age, status, created_at 
        FROM user 
        WHERE id = :id
    `)
    if err != nil {
        return nil, err
    }
    
    return repo, nil
}

func (r *UserRepository) Close() error {
    if r.findStmt != nil {
        r.findStmt.Close()
    }
    if r.getByIDStmt != nil {
        r.getByIDStmt.Close()
    }
    return nil
}

func (r *UserRepository) Find(ctx context.Context, name, status string) ([]User, error) {
    var users []User
    err := r.findStmt.Select(ctx, &users, map[string]interface{}{
        "name":   name,
        "status": status,
    })
    return users, err
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (*User, error) {
    var user User
    err := r.getByIDStmt.Get(ctx, &user, map[string]interface{}{
        "id": id,
    })
    if err != nil {
        return nil, err
    }
    return &user, nil
}
```

---

## 参数类型支持

### 使用 map 作为参数

```go
params := map[string]interface{}{
    "name":   "张三",
    "age":    25,
    "status": "active",
    "ids":    []int64{1, 2, 3},
}

engine.Select(ctx, &users, sql, params)
```

### 使用结构体作为参数

```go
type UserQuery struct {
    Name   string `db:"name"`
    MinAge int    `db:"min_age"`
    MaxAge int    `db:"max_age"`
    Status string `db:"status"`
}

func findUsersByStruct(ctx context.Context, engine *sqlx.Engine, query UserQuery) ([]User, error) {
    sql := `
    SELECT id, name, email, age, status, created_at 
    FROM user 
    WHERE 1=1
    #[ AND name LIKE :name ]
    #[ AND age >= :min_age ]
    #[ AND age <= :max_age ]
    #[ AND status = :status ]
    `
    
    var users []User
    err := engine.Select(ctx, &users, sql, query)
    return users, err
}

// 使用示例
users, _ := findUsersByStruct(ctx, engine, UserQuery{
    Name:   "张%",
    MinAge: 18,
    Status: "active",
})
```

### 使用指针类型区分"未设置"和"零值"

```go
type UserQueryWithPtr struct {
    Name   string  `db:"name"`
    Age    *int    `db:"age"`     // 使用指针区分 0 和 nil
    Active *bool   `db:"active"`  // 使用指针区分 false 和 nil
}

func main() {
    // 场景：要查询 age = 0 的用户
    age := 0
    users, _ := findUsers(ctx, engine, UserQueryWithPtr{Age: &age})
    // 条件生效: AND age = 0
    
    // 场景：不想按 age 过滤
    users, _ := findUsers(ctx, engine, UserQueryWithPtr{Age: nil})
    // 条件移除
}
```

---

## 条件判断规则

`#[ ]` 块的保留/移除规则：

| 参数值 | 是否保留条件 | 说明 |
|--------|-------------|------|
| `nil` | ❌ 移除 | 参数不存在或为nil |
| 空字符串 `""` | ❌ 移除 | 字符串为空 |
| 非空字符串 | ✅ 保留 | 有实际内容 |
| 零值 `0` | ❌ 移除 | int/float等零值 |
| 非零值 | ✅ 保留 | 有实际数值 |
| `false` | ❌ 移除 | 布尔false |
| `true` | ✅ 保留 | 布尔true |
| 空切片 `[]int{}` | ❌ 移除 | 切片长度为0 |
| 非空切片 | ✅ 保留 | 有元素 |
| 空map | ❌ 移除 | map长度为0 |
| 非空map | ✅ 保留 | 有键值对 |
| `nil` 指针 | ❌ 移除 | 指针为nil |
| 非nil指针 | ✅ 保留 | 指针有值（即使指向零值）|

### 特殊说明

**指针类型的特殊处理**：非nil指针总是被视为"有值"，即使指向零值。这允许你区分"用户明确设置为0"和"用户未设置"两种情况。

```go
// 不使用指针 - 无法区分 0 和 "未设置"
type Query1 struct {
    Age int `db:"age"`
}
// Age = 0 时，条件被移除（可能不是期望的行为）

// 使用指针 - 可以区分
type Query2 struct {
    Age *int `db:"age"`
}
// Age = &0 时，条件保留（用户明确要查 age=0）
// Age = nil 时，条件移除（用户未设置）
```

---

## 最佳实践

### 1. 始终使用 `WHERE 1=1`

```sql
-- ✅ 推荐
WHERE 1=1
#[ AND name = :name ]
#[ AND age >= :age ]

-- ❌ 不推荐（第一个条件需要处理 AND）
WHERE #[ name = :name ]#[ AND age >= :age ]
```

### 2. 合理使用动态插入

```go
// 对于可选字段很多的表，使用动态插入
sql := `
INSERT INTO user (
    name
    #[ , nickname ]
    #[ , avatar ]
    #[ , bio ]
    #[ , website ]
) VALUES (
    :name
    #[ , :nickname ]
    #[ , :avatar ]
    #[ , :bio ]
    #[ , :website ]
)
`
```

### 3. 使用结构体封装查询参数

```go
// 定义查询结构体，便于复用和类型安全
type UserListQuery struct {
    Name     string   `db:"name"`
    Status   string   `db:"status"`
    MinAge   int      `db:"min_age"`
    MaxAge   int      `db:"max_age"`
    IDs      []int64  `db:"ids"`
    Page     int      `db:"page"`
    PageSize int      `db:"page_size"`
}
```

### 4. 日志记录生成的SQL

```go
// 在开发环境打印生成的SQL
func debugSelect(ctx context.Context, engine *sqlx.Engine, dest interface{}, 
    query string, arg interface{}) error {
    
    processedQuery := sqlx.Preprocess(query, arg)
    q, args, _ := sqlx.Named(processedQuery, arg)
    log.Printf("[SQL] %s | args: %v", q, args)
    
    return engine.Select(ctx, dest, query, arg)
}
```

---

## 完整示例

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/tfnick/sqlx"
)

// 模型定义
type User struct {
    ID        int64     `db:"id"`
    Name      string    `db:"name"`
    Email     string    `db:"email"`
    Age       int       `db:"age"`
    Status    string    `db:"status"`
    CreatedAt time.Time `db:"created_at"`
}

// 查询参数
type UserQuery struct {
    Name   string  `db:"name"`
    Status string  `db:"status"`
    MinAge int     `db:"min_age"`
    MaxAge int     `db:"max_age"`
    IDs    []int64 `db:"ids"`
}

// 用户仓库
type UserRepo struct {
    engine *sqlx.Engine
}

func NewUserRepo(db *sqlx.DB) *UserRepo {
    return &UserRepo{engine: sqlx.NewEngine(db)}
}

// 动态查询
func (r *UserRepo) Find(ctx context.Context, q UserQuery) ([]User, error) {
    sql := `
    SELECT id, name, email, age, status, created_at 
    FROM user 
    WHERE 1=1
    #[ AND name LIKE :name ]
    #[ AND status = :status ]
    #[ AND age >= :min_age ]
    #[ AND age <= :max_age ]
    #[ AND id IN :ids ]
    ORDER BY created_at DESC
    `
    
    var users []User
    err := r.engine.Select(ctx, &users, sql, q)
    return users, err
}

// 创建用户
func (r *UserRepo) Create(ctx context.Context, user *User) (int64, error) {
    sql := `
    INSERT INTO user (name, email, age, status, created_at)
    VALUES (:name, :email, :age, :status, NOW())
    RETURNING id
    `
    
    var id int64
    err := r.engine.Get(ctx, &id, sql, user)
    return id, err
}

// 动态更新
func (r *UserRepo) Update(ctx context.Context, id int64, updates map[string]interface{}) error {
    sql := `
    UPDATE user SET
        updated_at = NOW()
        #[ , name = :name ]
        #[ , email = :email ]
        #[ , age = :age ]
        #[ , status = :status ]
    WHERE id = :id
    `
    
    updates["id"] = id
    _, err := r.engine.Exec(ctx, sql, updates)
    return err
}

// 删除
func (r *UserRepo) Delete(ctx context.Context, id int64) error {
    sql := `DELETE FROM user WHERE id = :id`
    _, err := r.engine.Exec(ctx, sql, map[string]interface{}{"id": id})
    return err
}

func main() {
    // 连接数据库
    db, err := sqlx.Open("postgres", "postgres://user:pass@localhost/db?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()
    
    repo := NewUserRepo(db)
    ctx := context.Background()
    
    // 动态查询示例
    users, err := repo.Find(ctx, UserQuery{
        Name:   "张%",
        Status: "active",
        MinAge: 18,
        IDs:    []int64{1, 2, 3, 4, 5},
    })
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Found %d users\n", len(users))
    for _, u := range users {
        fmt.Printf("  - %s (%s)\n", u.Name, u.Email)
    }
}
```
