# Manager 统一入口

## 目标

客户端只用一个入口：`*Manager`。不再需要知道 DB 和 Engine 的存在。

## 策略

Manager 的 `Get`/`Select`/`Exec`/`Queryx`/`QueryRowx` 内部分析查询语句：

- 含 `:param` 模式 → 走 Engine（命名参数 + 动态 SQL）
- 否则 → 走 DB（位置参数 `?`）

同时补 `WithTransaction` 到 Manager，默认用 "app" 库。

## 客户端代码

```go
mgr := sqlx.NewManager()
mgr.MustOpen("app", "sqlite3", "app.db")

// 自动识别为命名参数 → Engine
mgr.Get(&a, "SELECT * FROM t WHERE id = :id", map[string]interface{}{"id": 1})

// 自动识别为位置参数 → DB
mgr.Get(&a, "SELECT * FROM t WHERE id = ?", 1)

// 无参数也行
mgr.Select(&list, "SELECT * FROM t ORDER BY id")

// 事务
mgr.WithTransaction(func(tx *sqlx.Tx) error { ... })
```

## 变更

| 文件 | 内容 |
|------|------|
| `manager.go` | Get/Select/Exec/Queryx/QueryRowx 改为自动路由；新增 WithTransaction |
| `manager_test.go` | 对应测试 |
