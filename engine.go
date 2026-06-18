package sqlx

import (
	"context"
	"database/sql"
	"errors"
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
//	err := engine.Select(&users, sql, params)
//
// Example without parameters:
//
//	sql := `SELECT * FROM users ORDER BY created_at DESC`
//	var users []User
//	err := engine.Select(&users, sql)
type Engine struct {
	db *DB
	tx *Tx
}

// NewEngine creates a new Engine wrapping the given sqlx.DB.
func NewEngine(db *DB) *Engine {
	return &Engine{db: db}
}

func newTxEngine(tx *Tx) *Engine {
	return &Engine{tx: tx}
}

// DB returns the underlying sqlx.DB for internal integration and tests.
// Application code should get and pass around *Engine rather than using this
// lower-level handle directly.
func (e *Engine) DB() *DB {
	return e.db
}

// StdDB returns the underlying standard library *sql.DB.
// It returns nil for transaction-bound Engines.
func (e *Engine) StdDB() *sql.DB {
	if e.db == nil {
		return nil
	}
	return e.db.StdDB()
}

func (e *Engine) driverName() string {
	if e.tx != nil {
		return e.tx.DriverName()
	}
	return e.db.DriverName()
}

func (e *Engine) mapper() *reflectx.Mapper {
	if e.tx != nil {
		return e.tx.Mapper
	}
	return e.db.Mapper
}

func (e *Engine) rebind(query string) string {
	if e.tx != nil {
		return e.tx.Rebind(query)
	}
	return e.db.Rebind(query)
}

func (e *Engine) selectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if e.tx != nil {
		return e.tx.SelectContext(ctx, dest, query, args...)
	}
	return e.db.SelectContext(ctx, dest, query, args...)
}

func (e *Engine) getContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if e.tx != nil {
		return e.tx.GetContext(ctx, dest, query, args...)
	}
	return e.db.GetContext(ctx, dest, query, args...)
}

func (e *Engine) execContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if e.tx != nil {
		return e.tx.ExecContext(ctx, query, args...)
	}
	return e.db.ExecContext(ctx, query, args...)
}

func (e *Engine) queryxContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	if e.tx != nil {
		return e.tx.QueryxContext(ctx, query, args...)
	}
	return e.db.QueryxContext(ctx, query, args...)
}

func (e *Engine) queryRowxContext(ctx context.Context, query string, args ...interface{}) *Row {
	if e.tx != nil {
		return e.tx.QueryRowxContext(ctx, query, args...)
	}
	return e.db.QueryRowxContext(ctx, query, args...)
}

func (e *Engine) prepareNamedContext(ctx context.Context, query string) (*NamedStmt, error) {
	if e.tx != nil {
		return e.tx.PrepareNamedContext(ctx, query)
	}
	return e.db.PrepareNamedContext(ctx, query)
}

func (e *Engine) preparexContext(ctx context.Context, query string) (*Stmt, error) {
	if e.tx != nil {
		return e.tx.PreparexContext(ctx, query)
	}
	return e.db.PreparexContext(ctx, query)
}

// ExecPContext executes positional SQL using portable ? placeholders.
func (e *Engine) ExecPContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return e.execContext(ctx, e.rebind(query), args...)
}

// ExecP executes positional SQL using portable ? placeholders.
func (e *Engine) ExecP(query string, args ...interface{}) (sql.Result, error) {
	return e.ExecPContext(context.Background(), query, args...)
}

// GetPContext executes positional SQL and scans one row.
func (e *Engine) GetPContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return e.getContext(ctx, dest, e.rebind(query), args...)
}

// GetP executes positional SQL and scans one row.
func (e *Engine) GetP(dest interface{}, query string, args ...interface{}) error {
	return e.GetPContext(context.Background(), dest, query, args...)
}

// SelectPContext executes positional SQL and scans all rows.
func (e *Engine) SelectPContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return e.selectContext(ctx, dest, e.rebind(query), args...)
}

// SelectP executes positional SQL and scans all rows.
func (e *Engine) SelectP(dest interface{}, query string, args ...interface{}) error {
	return e.SelectPContext(context.Background(), dest, query, args...)
}

// ExecNamedContext executes named SQL using :name placeholders.
func (e *Engine) ExecNamedContext(ctx context.Context, query string, arg interface{}) (sql.Result, error) {
	q, args, err := bindNamedMapper(BindType(e.driverName()), query, arg, e.mapper())
	if err != nil {
		return nil, err
	}
	return e.execContext(ctx, q, args...)
}

// ExecNamed executes named SQL using :name placeholders.
func (e *Engine) ExecNamed(query string, arg interface{}) (sql.Result, error) {
	return e.ExecNamedContext(context.Background(), query, arg)
}

// GetNamedContext executes named SQL and scans one row.
func (e *Engine) GetNamedContext(ctx context.Context, dest interface{}, query string, arg interface{}) error {
	q, args, err := bindNamedMapper(BindType(e.driverName()), query, arg, e.mapper())
	if err != nil {
		return err
	}
	return e.getContext(ctx, dest, q, args...)
}

// GetNamed executes named SQL and scans one row.
func (e *Engine) GetNamed(dest interface{}, query string, arg interface{}) error {
	return e.GetNamedContext(context.Background(), dest, query, arg)
}

// SelectNamedContext executes named SQL and scans all rows.
func (e *Engine) SelectNamedContext(ctx context.Context, dest interface{}, query string, arg interface{}) error {
	q, args, err := bindNamedMapper(BindType(e.driverName()), query, arg, e.mapper())
	if err != nil {
		return err
	}
	return e.selectContext(ctx, dest, q, args...)
}

// SelectNamed executes named SQL and scans all rows.
func (e *Engine) SelectNamed(dest interface{}, query string, arg interface{}) error {
	return e.SelectNamedContext(context.Background(), dest, query, arg)
}

func firstArg(arg []interface{}) interface{} {
	if len(arg) == 0 {
		return nil
	}
	return arg[0]
}

func (e *Engine) prepareDynamic(query string, arg interface{}) (string, []interface{}, error) {
	query = Preprocess(query, arg)
	query = normalizeInNamedParams(query)
	if arg == nil {
		return query, nil, nil
	}
	q, args, err := bindNamedMapper(QUESTION, query, arg, e.mapper())
	if err != nil {
		return "", nil, err
	}
	q, args, err = In(q, args...)
	if err != nil {
		return "", nil, err
	}
	return e.rebind(q), args, nil
}

// Select executes a query with dynamic SQL support and scans results into dest.
// The query supports #[ ] conditional blocks that are removed when the
// corresponding named parameter is nil, empty, or not present.
//
// dest must be a pointer to a slice.
// arg is optional; pass nil or omit when there are no parameters.
func (e *Engine) Select(dest interface{}, query string, arg ...interface{}) error {
	return e.SelectContext(context.Background(), dest, query, arg...)
}

// SelectContext executes a query with dynamic SQL support and scans results into dest.
func (e *Engine) SelectContext(ctx context.Context, dest interface{}, query string, arg ...interface{}) error {
	q, args, err := e.prepareDynamic(query, firstArg(arg))
	if err != nil {
		return err
	}
	return e.selectContext(ctx, dest, q, args...)
}

// Get executes a query with dynamic SQL support and scans a single row into dest.
// The query supports #[ ] conditional blocks that are removed when the
// corresponding named parameter is nil, empty, or not present.
//
// dest must be a pointer to a struct or scannable type.
// Returns sql.ErrNoRows if no result is found.
// arg is optional; pass nil or omit when there are no parameters.
func (e *Engine) Get(dest interface{}, query string, arg ...interface{}) error {
	return e.GetContext(context.Background(), dest, query, arg...)
}

// GetContext executes a query with dynamic SQL support and scans one row into dest.
func (e *Engine) GetContext(ctx context.Context, dest interface{}, query string, arg ...interface{}) error {
	q, args, err := e.prepareDynamic(query, firstArg(arg))
	if err != nil {
		return err
	}
	return e.getContext(ctx, dest, q, args...)
}

// Exec executes a query with dynamic SQL support.
// The query supports #[ ] conditional blocks that are removed when the
// corresponding named parameter is nil, empty, or not present.
// arg is optional; pass nil or omit when there are no parameters.
func (e *Engine) Exec(query string, arg ...interface{}) (sql.Result, error) {
	return e.ExecContext(context.Background(), query, arg...)
}

// ExecContext executes a query with dynamic SQL support.
func (e *Engine) ExecContext(ctx context.Context, query string, arg ...interface{}) (sql.Result, error) {
	q, args, err := e.prepareDynamic(query, firstArg(arg))
	if err != nil {
		return nil, err
	}
	return e.execContext(ctx, q, args...)
}

// MustExec executes a query with dynamic SQL support and panics on error.
func (e *Engine) MustExec(query string, arg ...interface{}) sql.Result {
	res, err := e.Exec(query, arg...)
	if err != nil {
		panic(err)
	}
	return res
}

// Queryx executes a query with dynamic SQL support and returns *sqlx.Rows.
// The query supports #[ ] conditional blocks.
// arg is optional; pass nil or omit when there are no parameters.
func (e *Engine) Queryx(query string, arg ...interface{}) (*Rows, error) {
	return e.QueryxContext(context.Background(), query, arg...)
}

// QueryxContext executes a query with dynamic SQL support and returns *sqlx.Rows.
func (e *Engine) QueryxContext(ctx context.Context, query string, arg ...interface{}) (*Rows, error) {
	q, args, err := e.prepareDynamic(query, firstArg(arg))
	if err != nil {
		return nil, err
	}
	return e.queryxContext(ctx, q, args...)
}

// QueryRowx executes a query with dynamic SQL support and returns *sqlx.Row.
// The query supports #[ ] conditional blocks.
// arg is optional; pass nil or omit when there are no parameters.
func (e *Engine) QueryRowx(query string, arg ...interface{}) *Row {
	return e.QueryRowxContext(context.Background(), query, arg...)
}

// QueryRowxContext executes a query with dynamic SQL support and returns *sqlx.Row.
func (e *Engine) QueryRowxContext(ctx context.Context, query string, arg ...interface{}) *Row {
	q, args, err := e.prepareDynamic(query, firstArg(arg))
	if err != nil {
		return &Row{err: err}
	}
	return e.queryRowxContext(ctx, q, args...)
}

// DynNamedStmt is a prepared statement with dynamic SQL support.
type DynNamedStmt struct {
	Params      []string
	QueryString string
	Stmt        *Stmt
	engine      *Engine
	dynamic     bool
}

// PrepareNamed prepares a named statement with dynamic SQL support.
func (e *Engine) PrepareNamed(query string) (*DynNamedStmt, error) {
	return e.PrepareNamedContext(context.Background(), query)
}

// PrepareNamedContext prepares a named statement with dynamic SQL support.
func (e *Engine) PrepareNamedContext(ctx context.Context, query string) (*DynNamedStmt, error) {
	// Note: We don't preprocess here because we need the original query
	// to extract named parameters. Preprocessing happens at execution time.
	if preprocessRegex.MatchString(query) {
		return &DynNamedStmt{
			QueryString: query,
			engine:      e,
			dynamic:     true,
		}, nil
	}
	stmt, err := e.prepareNamedContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &DynNamedStmt{
		Params:      stmt.Params,
		QueryString: query,
		Stmt:        stmt.Stmt,
		engine:      e,
		dynamic:     preprocessRegex.MatchString(query),
	}, nil
}

// Select executes the prepared statement with dynamic SQL support.
// arg is optional; pass nil or omit when there are no parameters.
func (s *DynNamedStmt) Select(dest interface{}, arg ...interface{}) error {
	return s.SelectContext(context.Background(), dest, arg...)
}

// SelectContext executes the prepared statement with dynamic SQL support.
func (s *DynNamedStmt) SelectContext(ctx context.Context, dest interface{}, arg ...interface{}) error {
	if !s.dynamic {
		a := firstArg(arg)
		if a == nil {
			return s.Stmt.SelectContext(ctx, dest)
		}
		args, err := bindAnyArgs(s.Params, a, s.Stmt.Mapper)
		if err != nil {
			return err
		}
		return s.Stmt.SelectContext(ctx, dest, args...)
	}
	q, args, err := s.engine.prepareDynamic(s.QueryString, firstArg(arg))
	if err != nil {
		return err
	}
	return s.engine.selectContext(ctx, dest, q, args...)
}

// Get executes the prepared statement with dynamic SQL support for a single row.
// arg is optional; pass nil or omit when there are no parameters.
func (s *DynNamedStmt) Get(dest interface{}, arg ...interface{}) error {
	return s.GetContext(context.Background(), dest, arg...)
}

// GetContext executes the prepared statement with dynamic SQL support for one row.
func (s *DynNamedStmt) GetContext(ctx context.Context, dest interface{}, arg ...interface{}) error {
	if !s.dynamic {
		a := firstArg(arg)
		if a == nil {
			return s.Stmt.GetContext(ctx, dest)
		}
		args, err := bindAnyArgs(s.Params, a, s.Stmt.Mapper)
		if err != nil {
			return err
		}
		return s.Stmt.GetContext(ctx, dest, args...)
	}
	q, args, err := s.engine.prepareDynamic(s.QueryString, firstArg(arg))
	if err != nil {
		return err
	}
	return s.engine.getContext(ctx, dest, q, args...)
}

// Exec executes the prepared statement with dynamic SQL support.
// arg is optional; pass nil or omit when there are no parameters.
func (s *DynNamedStmt) Exec(arg ...interface{}) (sql.Result, error) {
	return s.ExecContext(context.Background(), arg...)
}

// ExecContext executes the prepared statement with dynamic SQL support.
func (s *DynNamedStmt) ExecContext(ctx context.Context, arg ...interface{}) (sql.Result, error) {
	if !s.dynamic {
		a := firstArg(arg)
		if a == nil {
			return s.Stmt.ExecContext(ctx)
		}
		args, err := bindAnyArgs(s.Params, a, s.Stmt.Mapper)
		if err != nil {
			return nil, err
		}
		return s.Stmt.ExecContext(ctx, args...)
	}
	q, args, err := s.engine.prepareDynamic(s.QueryString, firstArg(arg))
	if err != nil {
		return nil, err
	}
	return s.engine.execContext(ctx, q, args...)
}

// Close closes the prepared statement.
func (s *DynNamedStmt) Close() error {
	if s.Stmt == nil {
		return nil
	}
	return s.Stmt.Close()
}

// WithTransaction executes fn within a transaction-bound Engine.
func (e *Engine) WithTransaction(fn func(*Engine) error) error {
	return e.WithTransactionContext(context.Background(), nil, fn)
}

// WithTransactionContext executes fn within a transaction-bound Engine.
func (e *Engine) WithTransactionContext(ctx context.Context, opts *sql.TxOptions, fn func(*Engine) error) (err error) {
	if e.tx != nil {
		return errors.New("sqlx: nested Engine transactions are not supported")
	}
	tx, err := e.db.BeginTxx(ctx, opts)
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
	err = fn(newTxEngine(tx))
	return
}

// WithTransactionRaw executes fn within a transaction-bound Engine and also
// passes the underlying standard library *sql.Tx for integration with
// libraries that require it.
func (e *Engine) WithTransactionRaw(ctx context.Context, opts *sql.TxOptions, fn func(*Engine, *sql.Tx) error) (err error) {
	if e.tx != nil {
		return errors.New("sqlx: nested Engine transactions are not supported")
	}
	tx, err := e.db.BeginTxx(ctx, opts)
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
	err = fn(newTxEngine(tx), tx.StdTx())
	return
}

// WithTransaction executes fn within a database transaction using the
// lower-level Tx wrapper. New application code should prefer
// Engine.WithTransaction so repositories receive *Engine in both normal and
// transactional flows.
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

var inNamedParamRegex = regexp.MustCompile(`(?i)\bIN\s+:([a-zA-Z_][a-zA-Z0-9_.]*)`)

func normalizeInNamedParams(query string) string {
	return inNamedParamRegex.ReplaceAllString(query, "IN (:$1)")
}

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

	// Try string-keyed maps first, including named map types convertible to
	// map[string]interface{}.
	if m, ok := convertMapStringInterface(arg); ok {
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
