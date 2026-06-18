package sqlx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/tfnick/sqlx/reflectx"
)

// ErrUnsupportedDialect is returned when a CRUD helper is not implemented for
// the current database dialect.
var ErrUnsupportedDialect = errors.New("sqlx: unsupported dialect")

var identifierRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// CrudOption configures Engine CRUD helpers.
type CrudOption func(*crudConfig)

type whereClause struct {
	column string
	value  interface{}
}

type orderClause struct {
	column string
	desc   bool
}

type crudConfig struct {
	columns       []string
	insertColumns []string
	updateColumns []string
	keys          []string
	conflictKeys  []string
	returning     []string
	wheres        []whereClause
	orderBy       string
	orders        []orderClause
	limit         int
	offset        int
	hasLimit      bool
	allowAllRows  bool
	batchSize     int
}

// Columns sets the columns used by simple CRUD helpers.
func Columns(names ...string) CrudOption {
	return func(c *crudConfig) { c.columns = append([]string(nil), names...) }
}

// InsertColumns sets the columns used by Save and SaveMany inserts.
func InsertColumns(names ...string) CrudOption {
	return func(c *crudConfig) { c.insertColumns = append([]string(nil), names...) }
}

// UpdateColumns sets the columns used by Update, Save, and SaveMany updates.
func UpdateColumns(names ...string) CrudOption {
	return func(c *crudConfig) { c.updateColumns = append([]string(nil), names...) }
}

// Keys sets the key columns used by Update.
func Keys(names ...string) CrudOption {
	return func(c *crudConfig) { c.keys = append([]string(nil), names...) }
}

// ConflictKeys sets the unique key columns used by Save and SaveMany.
func ConflictKeys(names ...string) CrudOption {
	return func(c *crudConfig) { c.conflictKeys = append([]string(nil), names...) }
}

// Returning sets the returning columns for insert helpers.
func Returning(names ...string) CrudOption {
	return func(c *crudConfig) { c.returning = append([]string(nil), names...) }
}

// Where adds an equality predicate to GetBy, SelectBy, and Delete.
func Where(column string, value interface{}) CrudOption {
	return func(c *crudConfig) {
		c.wheres = append(c.wheres, whereClause{column: column, value: value})
	}
}

// OrderBy sets a trusted static ORDER BY clause for SelectBy.
func OrderBy(expr string) CrudOption {
	return func(c *crudConfig) { c.orderBy = expr }
}

// OrderAsc orders results by a validated column in ascending order.
func OrderAsc(column string) CrudOption {
	return func(c *crudConfig) {
		c.orders = append(c.orders, orderClause{column: column})
	}
}

// OrderDesc orders results by a validated column in descending order.
func OrderDesc(column string) CrudOption {
	return func(c *crudConfig) {
		c.orders = append(c.orders, orderClause{column: column, desc: true})
	}
}

// LimitOffset sets LIMIT and OFFSET for SelectBy.
func LimitOffset(limit int, offset int) CrudOption {
	return func(c *crudConfig) {
		c.limit = limit
		c.offset = offset
		c.hasLimit = true
	}
}

// AllowAllRows permits Delete, GetBy, or SelectBy without Where clauses.
func AllowAllRows() CrudOption {
	return func(c *crudConfig) { c.allowAllRows = true }
}

// BatchSize sets the maximum number of rows per generated batch statement.
func BatchSize(n int) CrudOption {
	return func(c *crudConfig) { c.batchSize = n }
}

func newCrudConfig(opts []CrudOption) crudConfig {
	var cfg crudConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

func validateIdentifier(name string) error {
	if !identifierRegex.MatchString(name) {
		return errors.New("sqlx: invalid SQL identifier " + name)
	}
	return nil
}

func validateIdentifiers(names []string) error {
	for _, name := range names {
		if err := validateIdentifier(name); err != nil {
			return err
		}
	}
	return nil
}

func requireColumns(names []string, what string) error {
	if len(names) == 0 {
		return errors.New("sqlx: " + what + " are required")
	}
	return validateIdentifiers(names)
}

func parseCrudConfig(table string, opts []CrudOption) (crudConfig, error) {
	if table == "" {
		return crudConfig{}, errors.New("sqlx: table name is required")
	}
	if err := validateIdentifier(table); err != nil {
		return crudConfig{}, err
	}
	cfg := newCrudConfig(opts)
	for _, cols := range [][]string{cfg.columns, cfg.insertColumns, cfg.updateColumns, cfg.keys, cfg.conflictKeys, cfg.returning} {
		if err := validateIdentifiers(cols); err != nil {
			return crudConfig{}, err
		}
	}
	for _, w := range cfg.wheres {
		if err := validateIdentifier(w.column); err != nil {
			return crudConfig{}, err
		}
	}
	for _, order := range cfg.orders {
		if err := validateIdentifier(order.column); err != nil {
			return crudConfig{}, err
		}
	}
	return cfg, nil
}

func insertColumns(cfg crudConfig) []string {
	if len(cfg.insertColumns) > 0 {
		return cfg.insertColumns
	}
	return cfg.columns
}

func updateColumns(cfg crudConfig) []string {
	if len(cfg.updateColumns) > 0 {
		return cfg.updateColumns
	}
	return cfg.columns
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

func columnList(cols []string) string {
	return strings.Join(cols, ", ")
}

func bindValueByName(arg interface{}, name string, m *reflectx.Mapper) (interface{}, error) {
	if arg == nil {
		return nil, errors.New("sqlx: nil arg")
	}
	if maparg, ok := convertMapStringInterface(arg); ok {
		val, ok := maparg[name]
		if !ok {
			return nil, fmt.Errorf("could not find name %s in %#v", name, arg)
		}
		return val, nil
	}
	v := reflect.ValueOf(arg)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, errors.New("sqlx: nil pointer arg")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("sqlx: expected struct or map arg, got %T", arg)
	}
	t := m.TypeMap(v.Type())
	fi, ok := t.Names[name]
	if !ok {
		return nil, fmt.Errorf("could not find name %s in %#v", name, arg)
	}
	return reflectx.FieldByIndexesReadOnly(v, fi.Index).Interface(), nil
}

func bindValues(arg interface{}, cols []string, m *reflectx.Mapper) ([]interface{}, error) {
	args := make([]interface{}, 0, len(cols))
	for _, col := range cols {
		val, err := bindValueByName(arg, col, m)
		if err != nil {
			return nil, err
		}
		args = append(args, val)
	}
	return args, nil
}

type valueBinder struct {
	cols       []string
	mapper     *reflectx.Mapper
	traversals sync.Map
}

func newValueBinder(cols []string, m *reflectx.Mapper) *valueBinder {
	return &valueBinder{
		cols:   append([]string(nil), cols...),
		mapper: m,
	}
}

func (b *valueBinder) bind(arg interface{}) ([]interface{}, error) {
	if arg == nil {
		return nil, errors.New("sqlx: nil arg")
	}
	if maparg, ok := convertMapStringInterface(arg); ok {
		return bindMapArgs(b.cols, maparg)
	}

	v := reflect.ValueOf(arg)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, errors.New("sqlx: nil pointer arg")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("sqlx: expected struct or map arg, got %T", arg)
	}

	traversals, err := b.traversalsFor(v.Type())
	if err != nil {
		return nil, err
	}
	args := make([]interface{}, 0, len(traversals))
	for _, traversal := range traversals {
		args = append(args, reflectx.FieldByIndexesReadOnly(v, traversal).Interface())
	}
	return args, nil
}

func (b *valueBinder) traversalsFor(t reflect.Type) ([][]int, error) {
	if cached, ok := b.traversals.Load(t); ok {
		return cached.([][]int), nil
	}

	traversals := make([][]int, len(b.cols))
	err := b.mapper.TraversalsByNameFunc(t, b.cols, func(i int, traversal []int) error {
		if len(traversal) == 0 {
			return fmt.Errorf("could not find name %s in %s", b.cols[i], t)
		}
		traversals[i] = append([]int(nil), traversal...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	actual, _ := b.traversals.LoadOrStore(t, traversals)
	return actual.([][]int), nil
}

func rowsValue(args interface{}) (reflect.Value, error) {
	if args == nil {
		return reflect.Value{}, errors.New("sqlx: nil batch arg")
	}
	v := reflect.ValueOf(args)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return reflect.Value{}, errors.New("sqlx: nil batch pointer")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return reflect.Value{}, fmt.Errorf("sqlx: expected slice or array batch arg, got %T", args)
	}
	if v.Len() == 0 {
		return reflect.Value{}, errors.New("sqlx: empty batch")
	}
	return v, nil
}

func appendSlice(dest interface{}, src interface{}) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return errors.New("sqlx: destination must be a non-nil pointer")
	}
	ds := dv.Elem()
	if ds.Kind() != reflect.Slice {
		return errors.New("sqlx: destination must point to a slice")
	}
	sv := reflect.ValueOf(src)
	if sv.Kind() != reflect.Ptr || sv.IsNil() {
		return errors.New("sqlx: source must be a non-nil pointer")
	}
	ss := sv.Elem()
	if ss.Kind() != reflect.Slice {
		return errors.New("sqlx: source must point to a slice")
	}
	if !ss.Type().AssignableTo(ds.Type()) {
		return fmt.Errorf("sqlx: cannot append %s to %s", ss.Type(), ds.Type())
	}
	ds.Set(reflect.AppendSlice(ds, ss))
	return nil
}

func buildInsertSQL(table string, cols []string, rows int, returning []string) string {
	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(table)
	b.WriteString(" (")
	b.WriteString(columnList(cols))
	b.WriteString(") VALUES ")
	row := "(" + placeholders(len(cols)) + ")"
	for i := 0; i < rows; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(row)
	}
	if len(returning) > 0 {
		b.WriteString(" RETURNING ")
		b.WriteString(columnList(returning))
	}
	return b.String()
}

func buildSaveSQL(table string, insertCols, updateCols, conflictKeys []string, rows int) string {
	var b strings.Builder
	b.WriteString(buildInsertSQL(table, insertCols, rows, nil))
	b.WriteString(" ON CONFLICT (")
	b.WriteString(columnList(conflictKeys))
	b.WriteString(") DO UPDATE SET ")
	for i, col := range updateCols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(col)
		b.WriteString(" = excluded.")
		b.WriteString(col)
	}
	return b.String()
}

func isSQLiteOrPostgres(driverName string) bool {
	return driverName == "sqlite3" || driverName == "sqlite" || driverName == "postgres" || driverName == "postgresql" || driverName == "pgx"
}

func isSQLite(driverName string) bool {
	return driverName == "sqlite3" || driverName == "sqlite"
}

func defaultMaxParams(driverName string) int {
	switch driverName {
	case "postgres", "postgresql", "pgx":
		return 65535
	default:
		return 999
	}
}

func batchSize(cfg crudConfig, driverName string, columns int) int {
	if cfg.batchSize > 0 {
		return cfg.batchSize
	}
	if columns <= 0 {
		return 1
	}
	n := defaultMaxParams(driverName) / columns
	if n < 1 {
		return 1
	}
	return n
}

func scanSingleValue(dest interface{}, val interface{}) error {
	if dest == nil {
		return errors.New("sqlx: nil destination")
	}
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return errors.New("sqlx: destination must be a non-nil pointer")
	}
	elem := v.Elem()
	value := reflect.ValueOf(val)
	if !value.IsValid() {
		elem.Set(reflect.Zero(elem.Type()))
		return nil
	}
	if value.Type().AssignableTo(elem.Type()) {
		elem.Set(value)
		return nil
	}
	if value.Type().ConvertibleTo(elem.Type()) {
		elem.Set(value.Convert(elem.Type()))
		return nil
	}
	return fmt.Errorf("sqlx: cannot assign %T to %T", val, dest)
}

// InsertContext inserts one row into table.
func (e *Engine) InsertContext(ctx context.Context, table string, arg interface{}, opts ...CrudOption) (sql.Result, error) {
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return nil, err
	}
	cols := insertColumns(cfg)
	if err := requireColumns(cols, "columns"); err != nil {
		return nil, err
	}
	binder := newValueBinder(cols, e.mapper())
	args, err := binder.bind(arg)
	if err != nil {
		return nil, err
	}
	return e.execContext(ctx, e.rebind(buildInsertSQL(table, cols, 1, nil)), args...)
}

// Insert inserts one row into table.
func (e *Engine) Insert(table string, arg interface{}, opts ...CrudOption) (sql.Result, error) {
	return e.InsertContext(context.Background(), table, arg, opts...)
}

// InsertReturningContext inserts one row and scans the returned value.
func (e *Engine) InsertReturningContext(ctx context.Context, dest interface{}, table string, arg interface{}, opts ...CrudOption) error {
	if !isSQLiteOrPostgres(e.driverName()) {
		return ErrUnsupportedDialect
	}
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return err
	}
	cols := insertColumns(cfg)
	if err := requireColumns(cols, "columns"); err != nil {
		return err
	}
	if err := requireColumns(cfg.returning, "returning columns"); err != nil {
		return err
	}
	binder := newValueBinder(cols, e.mapper())
	args, err := binder.bind(arg)
	if err != nil {
		return err
	}
	if isSQLite(e.driverName()) && len(cfg.returning) == 1 && isSQLiteLastInsertIDColumn(cfg.returning[0]) {
		res, err := e.execContext(ctx, e.rebind(buildInsertSQL(table, cols, 1, nil)), args...)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err == nil {
			return scanSingleValue(dest, id)
		}
	}
	return e.getContext(ctx, dest, e.rebind(buildInsertSQL(table, cols, 1, cfg.returning)), args...)
}

func isSQLiteLastInsertIDColumn(column string) bool {
	return strings.EqualFold(column, "id") || strings.EqualFold(column, "rowid")
}

// InsertReturning inserts one row and scans the returned value.
func (e *Engine) InsertReturning(dest interface{}, table string, arg interface{}, opts ...CrudOption) error {
	return e.InsertReturningContext(context.Background(), dest, table, arg, opts...)
}

// InsertManyContext inserts a batch of rows.
func (e *Engine) InsertManyContext(ctx context.Context, table string, args interface{}, opts ...CrudOption) (sql.Result, error) {
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return nil, err
	}
	cols := insertColumns(cfg)
	if err := requireColumns(cols, "columns"); err != nil {
		return nil, err
	}
	rows, err := rowsValue(args)
	if err != nil {
		return nil, err
	}
	chunkSize := batchSize(cfg, e.driverName(), len(cols))
	binder := newValueBinder(cols, e.mapper())
	var res sql.Result
	for start := 0; start < rows.Len(); start += chunkSize {
		end := start + chunkSize
		if end > rows.Len() {
			end = rows.Len()
		}
		arglist := make([]interface{}, 0, (end-start)*len(cols))
		for i := start; i < end; i++ {
			vals, err := binder.bind(rows.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			arglist = append(arglist, vals...)
		}
		res, err = e.execContext(ctx, e.rebind(buildInsertSQL(table, cols, end-start, nil)), arglist...)
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

// InsertMany inserts a batch of rows.
func (e *Engine) InsertMany(table string, args interface{}, opts ...CrudOption) (sql.Result, error) {
	return e.InsertManyContext(context.Background(), table, args, opts...)
}

// InsertManyReturningContext inserts a batch and scans returned values.
func (e *Engine) InsertManyReturningContext(ctx context.Context, dest interface{}, table string, args interface{}, opts ...CrudOption) error {
	if !isSQLiteOrPostgres(e.driverName()) {
		return ErrUnsupportedDialect
	}
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return err
	}
	cols := insertColumns(cfg)
	if err := requireColumns(cols, "columns"); err != nil {
		return err
	}
	if err := requireColumns(cfg.returning, "returning columns"); err != nil {
		return err
	}
	rows, err := rowsValue(args)
	if err != nil {
		return err
	}
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr || destValue.IsNil() || destValue.Elem().Kind() != reflect.Slice {
		return errors.New("sqlx: destination must point to a slice")
	}
	destValue.Elem().SetLen(0)
	chunkSize := batchSize(cfg, e.driverName(), len(cols))
	binder := newValueBinder(cols, e.mapper())
	for start := 0; start < rows.Len(); start += chunkSize {
		end := start + chunkSize
		if end > rows.Len() {
			end = rows.Len()
		}
		arglist := make([]interface{}, 0, (end-start)*len(cols))
		for i := start; i < end; i++ {
			vals, err := binder.bind(rows.Index(i).Interface())
			if err != nil {
				return err
			}
			arglist = append(arglist, vals...)
		}
		tmp := reflect.New(destValue.Elem().Type()).Interface()
		if err := e.selectContext(ctx, tmp, e.rebind(buildInsertSQL(table, cols, end-start, cfg.returning)), arglist...); err != nil {
			return err
		}
		if err := appendSlice(dest, tmp); err != nil {
			return err
		}
	}
	return nil
}

// InsertManyReturning inserts a batch and scans returned values.
func (e *Engine) InsertManyReturning(dest interface{}, table string, args interface{}, opts ...CrudOption) error {
	return e.InsertManyReturningContext(context.Background(), dest, table, args, opts...)
}

// UpdateContext updates one row or set of rows by key columns.
func (e *Engine) UpdateContext(ctx context.Context, table string, arg interface{}, opts ...CrudOption) (sql.Result, error) {
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return nil, err
	}
	cols := updateColumns(cfg)
	if err := requireColumns(cols, "update columns"); err != nil {
		return nil, err
	}
	if err := requireColumns(cfg.keys, "key columns"); err != nil {
		return nil, err
	}
	bindCols := append(append([]string{}, cols...), cfg.keys...)
	binder := newValueBinder(bindCols, e.mapper())
	var b strings.Builder
	b.WriteString("UPDATE ")
	b.WriteString(table)
	b.WriteString(" SET ")
	for i, col := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(col)
		b.WriteString(" = ?")
	}
	b.WriteString(" WHERE ")
	for i, key := range cfg.keys {
		if i > 0 {
			b.WriteString(" AND ")
		}
		b.WriteString(key)
		b.WriteString(" = ?")
	}
	arglist, err := binder.bind(arg)
	if err != nil {
		return nil, err
	}
	return e.execContext(ctx, e.rebind(b.String()), arglist...)
}

// Update updates one row or set of rows by key columns.
func (e *Engine) Update(table string, arg interface{}, opts ...CrudOption) (sql.Result, error) {
	return e.UpdateContext(context.Background(), table, arg, opts...)
}

func whereSQL(cfg crudConfig) (string, []interface{}, error) {
	if len(cfg.wheres) == 0 {
		if cfg.allowAllRows {
			return "", nil, nil
		}
		return "", nil, errors.New("sqlx: at least one where clause is required")
	}
	parts := make([]string, 0, len(cfg.wheres))
	args := make([]interface{}, 0, len(cfg.wheres))
	for _, w := range cfg.wheres {
		parts = append(parts, w.column+" = ?")
		args = append(args, w.value)
	}
	return strings.Join(parts, " AND "), args, nil
}

// DeleteContext deletes rows matching Where options.
func (e *Engine) DeleteContext(ctx context.Context, table string, opts ...CrudOption) (sql.Result, error) {
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return nil, err
	}
	where, args, err := whereSQL(cfg)
	if err != nil {
		return nil, err
	}
	query := "DELETE FROM " + table
	if where != "" {
		query += " WHERE " + where
	}
	return e.execContext(ctx, e.rebind(query), args...)
}

// Delete deletes rows matching Where options.
func (e *Engine) Delete(table string, opts ...CrudOption) (sql.Result, error) {
	return e.DeleteContext(context.Background(), table, opts...)
}

// GetByContext selects one row from table.
func (e *Engine) GetByContext(ctx context.Context, dest interface{}, table string, opts ...CrudOption) error {
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return err
	}
	cols := cfg.columns
	if len(cols) == 0 {
		cols = []string{"*"}
	}
	where, args, err := whereSQL(cfg)
	if err != nil {
		return err
	}
	query := "SELECT " + columnList(cols) + " FROM " + table
	if where != "" {
		query += " WHERE " + where
	}
	return e.getContext(ctx, dest, e.rebind(query), args...)
}

// GetBy selects one row from table.
func (e *Engine) GetBy(dest interface{}, table string, opts ...CrudOption) error {
	return e.GetByContext(context.Background(), dest, table, opts...)
}

// SelectByContext selects rows from table.
func (e *Engine) SelectByContext(ctx context.Context, dest interface{}, table string, opts ...CrudOption) error {
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return err
	}
	cols := cfg.columns
	if len(cols) == 0 {
		cols = []string{"*"}
	}
	where, args, err := whereSQL(cfg)
	if err != nil {
		return err
	}
	query := "SELECT " + columnList(cols) + " FROM " + table
	if where != "" {
		query += " WHERE " + where
	}
	if len(cfg.orders) > 0 {
		orders := make([]string, 0, len(cfg.orders))
		for _, order := range cfg.orders {
			direction := "ASC"
			if order.desc {
				direction = "DESC"
			}
			orders = append(orders, order.column+" "+direction)
		}
		query += " ORDER BY " + strings.Join(orders, ", ")
	} else if cfg.orderBy != "" {
		query += " ORDER BY " + cfg.orderBy
	}
	if cfg.hasLimit {
		query += " LIMIT ? OFFSET ?"
		args = append(args, cfg.limit, cfg.offset)
	}
	return e.selectContext(ctx, dest, e.rebind(query), args...)
}

// SelectBy selects rows from table.
func (e *Engine) SelectBy(dest interface{}, table string, opts ...CrudOption) error {
	return e.SelectByContext(context.Background(), dest, table, opts...)
}

// SaveContext inserts or updates one row.
func (e *Engine) SaveContext(ctx context.Context, table string, arg interface{}, opts ...CrudOption) (sql.Result, error) {
	if !isSQLiteOrPostgres(e.driverName()) {
		return nil, ErrUnsupportedDialect
	}
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return nil, err
	}
	insertCols := insertColumns(cfg)
	updateCols := updateColumns(cfg)
	if err := requireColumns(insertCols, "insert columns"); err != nil {
		return nil, err
	}
	if err := requireColumns(updateCols, "update columns"); err != nil {
		return nil, err
	}
	if err := requireColumns(cfg.conflictKeys, "conflict keys"); err != nil {
		return nil, err
	}
	binder := newValueBinder(insertCols, e.mapper())
	args, err := binder.bind(arg)
	if err != nil {
		return nil, err
	}
	return e.execContext(ctx, e.rebind(buildSaveSQL(table, insertCols, updateCols, cfg.conflictKeys, 1)), args...)
}

// Save inserts or updates one row.
func (e *Engine) Save(table string, arg interface{}, opts ...CrudOption) (sql.Result, error) {
	return e.SaveContext(context.Background(), table, arg, opts...)
}

// SaveManyContext inserts or updates a batch of rows.
func (e *Engine) SaveManyContext(ctx context.Context, table string, args interface{}, opts ...CrudOption) (sql.Result, error) {
	if !isSQLiteOrPostgres(e.driverName()) {
		return nil, ErrUnsupportedDialect
	}
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return nil, err
	}
	insertCols := insertColumns(cfg)
	updateCols := updateColumns(cfg)
	if err := requireColumns(insertCols, "insert columns"); err != nil {
		return nil, err
	}
	if err := requireColumns(updateCols, "update columns"); err != nil {
		return nil, err
	}
	if err := requireColumns(cfg.conflictKeys, "conflict keys"); err != nil {
		return nil, err
	}
	rows, err := rowsValue(args)
	if err != nil {
		return nil, err
	}
	chunkSize := batchSize(cfg, e.driverName(), len(insertCols))
	binder := newValueBinder(insertCols, e.mapper())
	var res sql.Result
	for start := 0; start < rows.Len(); start += chunkSize {
		end := start + chunkSize
		if end > rows.Len() {
			end = rows.Len()
		}
		arglist := make([]interface{}, 0, (end-start)*len(insertCols))
		for i := start; i < end; i++ {
			vals, err := binder.bind(rows.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			arglist = append(arglist, vals...)
		}
		res, err = e.execContext(ctx, e.rebind(buildSaveSQL(table, insertCols, updateCols, cfg.conflictKeys, end-start)), arglist...)
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

// SaveMany inserts or updates a batch of rows.
func (e *Engine) SaveMany(table string, args interface{}, opts ...CrudOption) (sql.Result, error) {
	return e.SaveManyContext(context.Background(), table, args, opts...)
}

// CrudStmt is a prepared CRUD statement.
type CrudStmt struct {
	engine *Engine
	stmt   *Stmt
	cfg    crudConfig
	binder *valueBinder
}

// PrepareInsertContext prepares an insert CRUD statement.
func (e *Engine) PrepareInsertContext(ctx context.Context, table string, opts ...CrudOption) (*CrudStmt, error) {
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return nil, err
	}
	if len(cfg.returning) > 0 && !isSQLiteOrPostgres(e.driverName()) {
		return nil, ErrUnsupportedDialect
	}
	cols := insertColumns(cfg)
	if err := requireColumns(cols, "columns"); err != nil {
		return nil, err
	}
	stmt, err := e.preparexContext(ctx, e.rebind(buildInsertSQL(table, cols, 1, cfg.returning)))
	if err != nil {
		return nil, err
	}
	return &CrudStmt{engine: e, stmt: stmt, cfg: cfg, binder: newValueBinder(cols, e.mapper())}, nil
}

// PrepareInsert prepares an insert CRUD statement.
func (e *Engine) PrepareInsert(table string, opts ...CrudOption) (*CrudStmt, error) {
	return e.PrepareInsertContext(context.Background(), table, opts...)
}

// PrepareSaveContext prepares a save CRUD statement.
func (e *Engine) PrepareSaveContext(ctx context.Context, table string, opts ...CrudOption) (*CrudStmt, error) {
	if !isSQLiteOrPostgres(e.driverName()) {
		return nil, ErrUnsupportedDialect
	}
	cfg, err := parseCrudConfig(table, opts)
	if err != nil {
		return nil, err
	}
	insertCols := insertColumns(cfg)
	updateCols := updateColumns(cfg)
	if err := requireColumns(insertCols, "insert columns"); err != nil {
		return nil, err
	}
	if err := requireColumns(updateCols, "update columns"); err != nil {
		return nil, err
	}
	if err := requireColumns(cfg.conflictKeys, "conflict keys"); err != nil {
		return nil, err
	}
	stmt, err := e.preparexContext(ctx, e.rebind(buildSaveSQL(table, insertCols, updateCols, cfg.conflictKeys, 1)))
	if err != nil {
		return nil, err
	}
	return &CrudStmt{engine: e, stmt: stmt, cfg: cfg, binder: newValueBinder(insertCols, e.mapper())}, nil
}

// PrepareSave prepares a save CRUD statement.
func (e *Engine) PrepareSave(table string, opts ...CrudOption) (*CrudStmt, error) {
	return e.PrepareSaveContext(context.Background(), table, opts...)
}

func (s *CrudStmt) bind(arg interface{}) ([]interface{}, error) {
	return s.binder.bind(arg)
}

// Exec executes a prepared CRUD statement once.
func (s *CrudStmt) Exec(arg interface{}) (sql.Result, error) {
	return s.ExecContext(context.Background(), arg)
}

// ExecContext executes a prepared CRUD statement once.
func (s *CrudStmt) ExecContext(ctx context.Context, arg interface{}) (sql.Result, error) {
	args, err := s.bind(arg)
	if err != nil {
		return nil, err
	}
	return s.stmt.ExecContext(ctx, args...)
}

// ExecMany executes a prepared CRUD statement for each batch row.
func (s *CrudStmt) ExecMany(args interface{}) (sql.Result, error) {
	return s.ExecManyContext(context.Background(), args)
}

// ExecManyContext executes a prepared CRUD statement for each batch row.
func (s *CrudStmt) ExecManyContext(ctx context.Context, args interface{}) (sql.Result, error) {
	rows, err := rowsValue(args)
	if err != nil {
		return nil, err
	}
	var res sql.Result
	for i := 0; i < rows.Len(); i++ {
		res, err = s.ExecContext(ctx, rows.Index(i).Interface())
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

// ExecReturning executes a prepared CRUD statement once and scans one returned value.
func (s *CrudStmt) ExecReturning(dest interface{}, arg interface{}) error {
	return s.ExecReturningContext(context.Background(), dest, arg)
}

// ExecReturningContext executes a prepared CRUD statement once and scans one returned value.
func (s *CrudStmt) ExecReturningContext(ctx context.Context, dest interface{}, arg interface{}) error {
	args, err := s.bind(arg)
	if err != nil {
		return err
	}
	if len(s.cfg.returning) == 0 {
		return errors.New("sqlx: returning columns are required")
	}
	return s.stmt.GetContext(ctx, dest, args...)
}

// Close closes the prepared CRUD statement.
func (s *CrudStmt) Close() error {
	return s.stmt.Close()
}
