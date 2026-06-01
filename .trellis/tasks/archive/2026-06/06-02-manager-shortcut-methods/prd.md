# Manager 增加 Get/Select/Exec 快捷方法

## 问题

当前写法需要两步：

```go
eng := mgr.MustEngine("app")
eng.Get(ctx, &account, sql, params)
```

## 目标

Manager 直接暴露快捷方法，内部自动使用默认 "app" 数据库的 Engine：

```go
mgr.Get(&account, sql, params)
mgr.Select(&users, sql, params)
mgr.Exec(sql, params)
```

内部用 `context.Background()` 调用 Engine，保持简洁 API（无 ctx 参数）。

需要指定非默认库时，仍用 `MustEngine("other").Get(...)`。

## API

```go
func (m *Manager) Get(dest interface{}, query string, arg ...interface{}) error
func (m *Manager) Select(dest interface{}, query string, arg ...interface{}) error
func (m *Manager) Exec(query string, arg ...interface{}) (sql.Result, error)
func (m *Manager) Queryx(query string, arg ...interface{}) (*Rows, error)
func (m *Manager) QueryRowx(query string, arg ...interface{}) *Row
```

## 变更

| 文件 | 内容 |
|------|------|
| `manager.go` | Manager 新增 5 个快捷方法 |
| `manager_test.go` | 对应测试 |
