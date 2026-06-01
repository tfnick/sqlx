# 简化 Engine 获取方式

## 问题

当前客户端要使用 Engine 的动态 SQL 能力，需要两步：

```go
eng, err := mgr.DB("app")
if err != nil {
    return nil, err
}
// 然后再用 eng.LazyEngine().Select(...)
```

或者用 Must 变体也得先拿到 DB：

```go
db := mgr.MustDB("app")
db.LazyEngine().Select(ctx, &users, sql, params)
```

## 方案

Manager 增加 `Engine(name)` / `MustEngine(name)`，内部封装 DB 获取 + LazyEngine：

```go
// Engine returns the Engine for the named database.
func (m *Manager) Engine(name string) (*Engine, error)

// MustEngine is like Engine but panics on error.
func (m *Manager) MustEngine(name string) *Engine
```

客户端代码简化为：

```go
eng := mgr.MustEngine("app")
eng.Get(&account, sql, params)
```

或带错误处理：

```go
eng, err := mgr.Engine("app")
if err != nil {
    return nil, fmt.Errorf("数据库不可用: %w", err)
}
eng.Get(&account, sql, params)
```

## 变更范围

| 文件 | 内容 |
|------|------|
| `manager.go` | Manager 新增 `Engine()` / `MustEngine()` |
| `manager_test.go` | 新增对应测试 |

## 完成标准

- [ ] `Engine()` / `MustEngine()` 实现
- [ ] 测试通过
- [ ] `go vet` 通过
