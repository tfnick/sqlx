package sqlx

import (
	"context"
	"database/sql"
	"reflect"
	"regexp"

	"github.com/tfnick/sqlx/reflectx"
)

// Engine wraps sqlx.DB and provides dynamic SQL capabilities.
// It supports conditional SQL blocks using #[ ] syntax, similar to sqltoy.
//
// Example with parameters:
//
//	sql := `
//	SELECT id, name, age
//	FROM user
//	WHERE 1=1
//	#[ AND name LIKE :name ]
//	#[ AND age >= :min_age ]
//	#[ AND age <= :max_age ]
//	ORDER BY id DESC
//	`
//
//	params := map[string]interface{}{
//	    "name": "%tom%",
//	    "max_age": 30,
//	}
//
//	var users []User
//	err := engine.Select(ctx, &users, sql, params)
//
// Example without parameters:
//
//	sql := `SELECT * FROM users ORDER BY created_at DESC`
//	var users []User
//	err := engine.Select(ctx, &users, sql)
type Engine struct {
	db *DB
}

// NewEngine creates a new Engine wrapping the given sqlx.DB.
func NewEngine(db *DB) *Engine {
	return &Engine{db: db}
}

// DB returns the underlying sqlx.DB.
func (e *Engine) DB() *DB {
	return e.db
}

// Select executes a query with dynamic SQL support and scans results into dest.
// The query supports #[ ] conditional blocks that are removed when the
// corresponding named parameter is nil, empty, or not present.
//
// dest must be a pointer to a slice.
// arg is optional; pass nil or omit when there are no parameters.
func (e *Engine) Select(ctx context.Context, dest interface{}, query string, arg ...interface{}) error {
	var a interface{}
	if len(arg) > 0 {
		a = arg[0]
	}
	query = Preprocess(query, a)
	q, args, err := Named(query, a)
	if err != nil {
		return err
	}
	q, args, err = In(q, args...)
	if err != nil {
		return err
	}
	q = e.db.Rebind(q)
	return e.db.SelectContext(ctx, dest, q, args...)
}

// Get executes a query with dynamic SQL support and scans a single row into dest.
// The query supports #[ ] conditional blocks that are removed when the
// corresponding named parameter is nil, empty, or not present.
//
// dest must be a pointer to a struct or scannable type.
// Returns sql.ErrNoRows if no result is found.
// arg is optional; pass nil or omit when there are no parameters.
func (e *Engine) Get(ctx context.Context, dest interface{}, query string, arg ...interface{}) error {
	var a interface{}
	if len(arg) > 0 {
		a = arg[0]
	}
	query = Preprocess(query, a)
	q, args, err := Named(query, a)
	if err != nil {
		return err
	}
	q, args, err = In(q, args...)
	if err != nil {
		return err
	}
	q = e.db.Rebind(q)
	return e.db.GetContext(ctx, dest, q, args...)
}

// Exec executes a query with dynamic SQL support.
// The query supports #[ ] conditional blocks that are removed when the
// corresponding named parameter is nil, empty, or not present.
// arg is optional; pass nil or omit when there are no parameters.
func (e *Engine) Exec(ctx context.Context, query string, arg ...interface{}) (sql.Result, error) {
	var a interface{}
	if len(arg) > 0 {
		a = arg[0]
	}
	query = Preprocess(query, a)
	q, args, err := Named(query, a)
	if err != nil {
		return nil, err
	}
	q, args, err = In(q, args...)
	if err != nil {
		return nil, err
	}
	q = e.db.Rebind(q)
	return e.db.ExecContext(ctx, q, args...)
}

// Queryx executes a query with dynamic SQL support and returns *sqlx.Rows.
// The query supports #[ ] conditional blocks.
// arg is optional; pass nil or omit when there are no parameters.
func (e *Engine) Queryx(ctx context.Context, query string, arg ...interface{}) (*Rows, error) {
	var a interface{}
	if len(arg) > 0 {
		a = arg[0]
	}
	query = Preprocess(query, a)
	q, args, err := Named(query, a)
	if err != nil {
		return nil, err
	}
	q, args, err = In(q, args...)
	if err != nil {
		return nil, err
	}
	q = e.db.Rebind(q)
	return e.db.QueryxContext(ctx, q, args...)
}

// QueryRowx executes a query with dynamic SQL support and returns *sqlx.Row.
// The query supports #[ ] conditional blocks.
// arg is optional; pass nil or omit when there are no parameters.
func (e *Engine) QueryRowx(ctx context.Context, query string, arg ...interface{}) *Row {
	var a interface{}
	if len(arg) > 0 {
		a = arg[0]
	}
	query = Preprocess(query, a)
	q, args, err := Named(query, a)
	if err != nil {
		return &Row{err: err}
	}
	q, args, err = In(q, args...)
	if err != nil {
		return &Row{err: err}
	}
	q = e.db.Rebind(q)
	return e.db.QueryRowxContext(ctx, q, args...)
}

// NamedStmt is a prepared statement with dynamic SQL support.
type DynNamedStmt struct {
	Params      []string
	QueryString string
	Stmt        *Stmt
	engine      *Engine
}

// PrepareNamed prepares a named statement with dynamic SQL support.
func (e *Engine) PrepareNamed(query string) (*DynNamedStmt, error) {
	// Note: We don't preprocess here because we need the original query
	// to extract named parameters. Preprocessing happens at execution time.
	stmt, err := e.db.PrepareNamed(query)
	if err != nil {
		return nil, err
	}
	return &DynNamedStmt{
		Params:      stmt.Params,
		QueryString: stmt.QueryString,
		Stmt:        stmt.Stmt,
		engine:      e,
	}, nil
}

// Select executes the prepared statement with dynamic SQL support.
// arg is optional; pass nil or omit when there are no parameters.
func (s *DynNamedStmt) Select(ctx context.Context, dest interface{}, arg ...interface{}) error {
	var a interface{}
	if len(arg) > 0 {
		a = arg[0]
	}
	query := Preprocess(s.QueryString, a)
	// Re-compile the query after preprocessing
	q, args, err := bindNamedMapper(BindType(s.engine.db.DriverName()), query, a, s.Stmt.Mapper)
	if err != nil {
		return err
	}
	q, args, err = In(q, args...)
	if err != nil {
		return err
	}
	q = s.engine.db.Rebind(q)
	return s.engine.db.SelectContext(ctx, dest, q, args...)
}

// Get executes the prepared statement with dynamic SQL support for a single row.
// arg is optional; pass nil or omit when there are no parameters.
func (s *DynNamedStmt) Get(ctx context.Context, dest interface{}, arg ...interface{}) error {
	var a interface{}
	if len(arg) > 0 {
		a = arg[0]
	}
	query := Preprocess(s.QueryString, a)
	q, args, err := bindNamedMapper(BindType(s.engine.db.DriverName()), query, a, s.Stmt.Mapper)
	if err != nil {
		return err
	}
	q, args, err = In(q, args...)
	if err != nil {
		return err
	}
	q = s.engine.db.Rebind(q)
	return s.engine.db.GetContext(ctx, dest, q, args...)
}

// Exec executes the prepared statement with dynamic SQL support.
// arg is optional; pass nil or omit when there are no parameters.
func (s *DynNamedStmt) Exec(ctx context.Context, arg ...interface{}) (sql.Result, error) {
	var a interface{}
	if len(arg) > 0 {
		a = arg[0]
	}
	query := Preprocess(s.QueryString, a)
	q, args, err := bindNamedMapper(BindType(s.engine.db.DriverName()), query, a, s.Stmt.Mapper)
	if err != nil {
		return nil, err
	}
	q, args, err = In(q, args...)
	if err != nil {
		return nil, err
	}
	q = s.engine.db.Rebind(q)
	return s.engine.db.ExecContext(ctx, q, args...)
}

// Close closes the prepared statement.
func (s *DynNamedStmt) Close() error {
	return s.Stmt.Close()
}

// WithTransaction executes fn within a database transaction. It begins a
// transaction, calls fn with the *Tx, and automatically commits if fn returns
// nil. If fn returns an error, the transaction is rolled back. If fn panics,
// the transaction is rolled back and the panic is re-raised.
//
// Example:
//
//	db.WithTransaction(func(tx *sqlx.Tx) error {
//	    _, err := tx.Exec("INSERT INTO users ...")
//	    return err
//	})
func (db *DB) WithTransaction(fn func(*Tx) error) (err error) {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		} else if err != nil {
			tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	err = fn(tx)
	return
}

// preprocessRegex matches #[ content ] pattern for conditional SQL blocks.
// It captures the content between #[ and ], including optional leading whitespace
// before the #[ marker.
var preprocessRegex = regexp.MustCompile(`\s*#\[\s*([\s\S]*?)\s*\]`)

// namedParamRegex matches :param_name patterns within SQL.
var namedParamRegex = regexp.MustCompile(`:([a-zA-Z_][a-zA-Z0-9_.]*)`)

// Preprocess handles #[ ] conditional blocks in the query.
// A block is kept only if at least one named parameter inside it has a non-nil, non-empty value.
// When a block is removed, the entire block including surrounding whitespace is removed.
//
// Example:
//
//	query := "WHERE 1=1 #[ AND name = :name ] #[ AND age >= :min_age ]"
//	params := map[string]interface{}{"name": "John"}
//	result := Preprocess(query, params)
//	// result: "WHERE 1=1 AND name = :name"
func Preprocess(query string, arg interface{}) string {
	params := extractParams(arg)

	return preprocessRegex.ReplaceAllStringFunc(query, func(match string) string {
		// Extract content between #[ and ]
		submatches := preprocessRegex.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return ""
		}
		content := submatches[1]

		// Find all named parameters in this block
		paramMatches := namedParamRegex.FindAllStringSubmatch(content, -1)

		// Check if any parameter has a meaningful value
		for _, pm := range paramMatches {
			paramName := pm[1]
			if hasValue(params, paramName) {
				// Return content with a leading space for clean SQL formatting
				return " " + content
			}
		}

		// No valid parameters found, remove this block (including leading whitespace)
		return ""
	})
}

// extractParams extracts parameters from various argument types.
// Supports map[string]interface{}, and structs with `db` tags.
func extractParams(arg interface{}) map[string]interface{} {
	if arg == nil {
		return nil
	}

	// Try map[string]interface{} first
	if m, ok := arg.(map[string]interface{}); ok {
		return m
	}

	// Handle structs using reflection
	rv := reflect.ValueOf(arg)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	if rv.Kind() == reflect.Struct {
		m := mapper()
		typeMap := m.TypeMap(rv.Type())
		result := make(map[string]interface{}, len(typeMap.Names))
		for name, fi := range typeMap.Names {
			if len(fi.Index) > 0 {
				field := reflectx.FieldByIndexesReadOnly(rv, fi.Index)
				result[name] = field.Interface()
			}
		}
		return result
	}

	return nil
}

// hasValue checks if a parameter exists and has a meaningful (non-nil, non-empty) value.
func hasValue(params map[string]interface{}, paramName string) bool {
	if params == nil {
		return false
	}

	val, exists := params[paramName]
	if !exists {
		return false
	}

	return isTruthy(val)
}

// isTruthy determines if a value is considered "truthy" (should keep the conditional block).
// A value is truthy if it is:
//   - not nil
//   - not a zero value for its type (e.g., "", 0, false, nil pointer)
//   - not an empty slice or map
//
// Special case: a non-nil pointer to a zero value (like &false, &0, &"")
// is considered truthy because the user explicitly set it.
func isTruthy(val interface{}) bool {
	if val == nil {
		return false
	}

	rv := reflect.ValueOf(val)

	// Check for nil pointer or interface
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return false
		}
		// Non-nil pointer is always truthy - user explicitly set a value
		return true
	case reflect.Slice, reflect.Map:
		return rv.Len() > 0
	case reflect.String:
		return rv.String() != ""
	case reflect.Bool:
		return rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() != 0
	}

	return true
}
