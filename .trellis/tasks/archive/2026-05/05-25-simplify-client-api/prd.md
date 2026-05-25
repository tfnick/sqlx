# 简化 sqlx 客户端调用体验

## 目标

为 sqlx 增加多数据库管理和统一调用入口，让客户端代码更简洁。

## 背景

当前 sqlx 的使用方式：
- `sqlx.Open(driver, dsn)` → `*DB`，单库，无多库管理
- `sqlx.NewEngine(db)` → `*Engine`，需手动包装
- `db.Beginx()` → `*Tx`，事务需手动 begin/commit/rollback

参考项目（htmx-hyperscript-starter）中客户端需要自行封装 `GetDB()` / `GetEngine()` / `WithTransaction()` 三个入口。本项目应将这些通用能力内置到 sqlx 中。

## 需求

### 1. 多数据库管理器 (`Manager`)

- 支持同时管理多个命名的数据库连接
- 默认数据库名为 `"app"`，`Get("")` 等价于 `Get("app")`
- 提供 `Open()` / `MustOpen()` 注册新数据库
- 提供 `Add()` 注册已存在的 `*DB`
- 提供 `Close()` 关闭所有数据库
- SQLite 多库场景为典型用例

### 2. 统一调用体验

在 `*DB` 上新增方法，减少客户端样板代码：

- **`LazyEngine()`**：懒初始化并返回 `*Engine`，线程安全
- **`WithTransaction(fn)`**：封装事务，自动 begin/commit/rollback + panic 恢复，**无需 context 参数**

客户端调用方式统一为：
```go
mgr := sqlx.NewManager()
mgr.MustOpen("app", "sqlite3", "app.db")
db := mgr.MustGet("app")

// 简单查询（? 占位符）
db.Get(&users, "SELECT * FROM users WHERE id = ?", id)

// 动态 SQL（:named 参数 + #[ ] 条件块）
db.LazyEngine().Select(ctx, &users, sql, params)

// 事务
db.WithTransaction(func(tx *sqlx.Tx) error {
    tx.Exec(...)
    return nil
})
```

### 3. 多数据库设计预留

`Manager.Open(name, driverName, dsn)` 的 `driverName` 参数已天然支持任意驱动：

```go
mgr.MustOpen("app", "sqlite3", "app.db")       // SQLite
mgr.MustOpen("app", "postgres", "...")          // PostgreSQL（预留）
mgr.MustOpen("app", "mysql", "...")             // MySQL（预留）
```

## 设计决策

| 决策 | 选择 | 原因 |
|------|------|------|
| Manager 放置位置 | sqlx 包内 | 简单，可访问未导出字段 |
| Engine 获取方式 | `LazyEngine()` | 明确表达懒初始化语义 |
| WithTransaction 签名 | 无 context 参数 | 与 DB 的 Query/Exec API 风格一致 |
| 默认数据库名 | `"app"` | 单库场景零配置 |

## 变更范围

| 文件 | 内容 |
|------|------|
| `manager.go` (新) | `Manager` 结构体 + 方法 |
| `manager_test.go` (新) | Manager 测试 |
| `sqlx.go` | `*DB` 新增 `LazyEngine()` 方法 |
| `engine.go` | `*DB` 新增 `WithTransaction()` 方法 |

## API 清单

```go
// Manager
func NewManager() *Manager
func (m *Manager) Open(name, driverName, dataSourceName string) error
func (m *Manager) MustOpen(name, driverName, dataSourceName string)
func (m *Manager) Add(name string, db *DB) error
func (m *Manager) Get(name string) (*DB, error)    // name="" → "app"
func (m *Manager) MustGet(name string) *DB          // name="" → "app"
func (m *Manager) Close() error

// DB 新增
func (db *DB) LazyEngine() *Engine
func (db *DB) WithTransaction(fn func(*Tx) error) error
```

## 完成标准

- [ ] `Manager` 实现并通过测试
- [ ] `LazyEngine()` 实现（线程安全的懒初始化）
- [ ] `WithTransaction()` 实现（自动 rollback + panic 恢复）
- [ ] `go test -race ./...` 通过
- [ ] `make lint` 通过
