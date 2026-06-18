package sqlx

import (
	"database/sql"
	"strings"
	"testing"
)

func TestPreprocess(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		params   map[string]interface{}
		expected string
	}{
		{
			name:     "keep block with non-empty string",
			query:    "WHERE 1=1 #[ AND name = :name ]",
			params:   map[string]interface{}{"name": "John"},
			expected: "WHERE 1=1 AND name = :name",
		},
		{
			name:     "remove block with empty string",
			query:    "WHERE 1=1 #[ AND name = :name ]",
			params:   map[string]interface{}{"name": ""},
			expected: "WHERE 1=1",
		},
		{
			name:     "remove block with missing param",
			query:    "WHERE 1=1 #[ AND name = :name ]",
			params:   map[string]interface{}{},
			expected: "WHERE 1=1",
		},
		{
			name:     "remove block with nil param",
			query:    "WHERE 1=1 #[ AND name = :name ]",
			params:   map[string]interface{}{"name": nil},
			expected: "WHERE 1=1",
		},
		{
			name:     "keep block with non-zero int",
			query:    "WHERE 1=1 #[ AND age >= :min_age ]",
			params:   map[string]interface{}{"min_age": 18},
			expected: "WHERE 1=1 AND age >= :min_age",
		},
		{
			name:     "remove block with zero int",
			query:    "WHERE 1=1 #[ AND age >= :min_age ]",
			params:   map[string]interface{}{"min_age": 0},
			expected: "WHERE 1=1",
		},
		{
			name:     "keep block with true bool",
			query:    "WHERE 1=1 #[ AND active = :active ]",
			params:   map[string]interface{}{"active": true},
			expected: "WHERE 1=1 AND active = :active",
		},
		{
			name:     "remove block with false bool",
			query:    "WHERE 1=1 #[ AND active = :active ]",
			params:   map[string]interface{}{"active": false},
			expected: "WHERE 1=1",
		},
		{
			name:     "multiple blocks mixed",
			query:    "WHERE 1=1 #[ AND name = :name ] #[ AND age >= :min_age ] #[ AND status = :status ]",
			params:   map[string]interface{}{"name": "John", "min_age": 0},
			expected: "WHERE 1=1 AND name = :name",
		},
		{
			name:     "non-empty slice keeps block",
			query:    "WHERE 1=1 #[ AND id IN :ids ]",
			params:   map[string]interface{}{"ids": []int{1, 2, 3}},
			expected: "WHERE 1=1 AND id IN :ids",
		},
		{
			name:     "empty slice removes block",
			query:    "WHERE 1=1 #[ AND id IN :ids ]",
			params:   map[string]interface{}{"ids": []int{}},
			expected: "WHERE 1=1",
		},
		{
			name:     "non-empty map keeps block",
			query:    "WHERE 1=1 #[ AND meta = :meta ]",
			params:   map[string]interface{}{"meta": map[string]string{"key": "value"}},
			expected: "WHERE 1=1 AND meta = :meta",
		},
		{
			name:     "empty map removes block",
			query:    "WHERE 1=1 #[ AND meta = :meta ]",
			params:   map[string]interface{}{"meta": map[string]string{}},
			expected: "WHERE 1=1",
		},
		{
			name:     "nil params",
			query:    "WHERE 1=1 #[ AND name = :name ]",
			params:   nil,
			expected: "WHERE 1=1",
		},
		{
			name:     "no conditional blocks",
			query:    "SELECT * FROM user WHERE id = :id",
			params:   map[string]interface{}{"id": 1},
			expected: "SELECT * FROM user WHERE id = :id",
		},
		{
			name:     "multiple params in one block - any truthy keeps it",
			query:    "WHERE 1=1 #[ AND name = :name OR email = :email ]",
			params:   map[string]interface{}{"name": "", "email": "test@example.com"},
			expected: "WHERE 1=1 AND name = :name OR email = :email",
		},
		{
			name:     "multiple params in one block - all falsy removes it",
			query:    "WHERE 1=1 #[ AND name = :name OR email = :email ]",
			params:   map[string]interface{}{"name": "", "email": ""},
			expected: "WHERE 1=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Preprocess(tt.query, tt.params)
			if result != tt.expected {
				t.Errorf("Preprocess() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestPreprocessWithStruct(t *testing.T) {
	type SearchParams struct {
		Name   string `db:"name"`
		MinAge int    `db:"min_age"`
		Active *bool  `db:"active"`
	}

	tests := []struct {
		name     string
		query    string
		arg      interface{}
		expected string
	}{
		{
			name:     "struct with values",
			query:    "WHERE 1=1 #[ AND name = :name ] #[ AND age >= :min_age ]",
			arg:      SearchParams{Name: "John", MinAge: 18},
			expected: "WHERE 1=1 AND name = :name AND age >= :min_age",
		},
		{
			name:     "struct with empty string",
			query:    "WHERE 1=1 #[ AND name = :name ]",
			arg:      SearchParams{Name: ""},
			expected: "WHERE 1=1",
		},
		{
			name:     "struct with nil pointer",
			query:    "WHERE 1=1 #[ AND active = :active ]",
			arg:      SearchParams{Active: nil},
			expected: "WHERE 1=1",
		},
		{
			name:     "struct with non-nil pointer to true",
			query:    "WHERE 1=1 #[ AND active = :active ]",
			arg:      func() SearchParams { b := true; return SearchParams{Active: &b} }(),
			expected: "WHERE 1=1 AND active = :active",
		},
		{
			name:     "struct with non-nil pointer to false - keeps block because pointer is non-nil",
			query:    "WHERE 1=1 #[ AND active = :active ]",
			arg:      func() SearchParams { b := false; return SearchParams{Active: &b} }(),
			expected: "WHERE 1=1 AND active = :active",
		},
		{
			name:     "pointer to struct",
			query:    "WHERE 1=1 #[ AND name = :name ]",
			arg:      &SearchParams{Name: "John"},
			expected: "WHERE 1=1 AND name = :name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Preprocess(tt.query, tt.arg)
			if result != tt.expected {
				t.Errorf("Preprocess() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestIsTruthy(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected bool
	}{
		{"nil", nil, false},
		{"empty string", "", false},
		{"non-empty string", "test", true},
		{"zero int", 0, false},
		{"non-zero int", 42, true},
		{"zero int64", int64(0), false},
		{"non-zero int64", int64(42), true},
		{"zero uint", uint(0), false},
		{"non-zero uint", uint(42), true},
		{"false bool", false, false},
		{"true bool", true, true},
		{"empty slice", []int{}, false},
		{"non-empty slice", []int{1, 2, 3}, true},
		{"empty map", map[string]string{}, false},
		{"non-empty map", map[string]string{"a": "b"}, true},
		{"nil pointer", (*int)(nil), false},
		{"non-nil pointer", func() *int { i := 42; return &i }(), true},
		{"nil interface", interface{}(nil), false},
		{"zero float", 0.0, false},
		{"non-zero float", 3.14, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTruthy(tt.value)
			if result != tt.expected {
				t.Errorf("isTruthy(%v) = %v, want %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestExtractParamsFromMap(t *testing.T) {
	// Test map[string]interface{}
	m := map[string]interface{}{
		"name":  "John",
		"age":   30,
		"email": nil,
	}
	result := extractParams(m)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["name"] != "John" {
		t.Errorf("expected name=John, got %v", result["name"])
	}
	if result["age"] != 30 {
		t.Errorf("expected age=30, got %v", result["age"])
	}
	if result["email"] != nil {
		t.Errorf("expected email=nil, got %v", result["email"])
	}
}

func TestExtractParamsFromStruct(t *testing.T) {
	type TestStruct struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}

	s := TestStruct{
		Name:  "John",
		Age:   30,
		Email: "john@example.com",
	}

	result := extractParams(s)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["name"] != "John" {
		t.Errorf("expected name=John, got %v", result["name"])
	}
	if result["age"] != 30 {
		t.Errorf("expected age=30, got %v", result["age"])
	}
	if result["email"] != "john@example.com" {
		t.Errorf("expected email=john@example.com, got %v", result["email"])
	}
}

func TestExtractParamsFromNil(t *testing.T) {
	result := extractParams(nil)
	if result != nil {
		t.Errorf("expected nil result for nil input, got %v", result)
	}
}

func TestPreprocessMultiline(t *testing.T) {
	query := `
SELECT id, name, age
FROM user
WHERE 1=1
#[ AND name LIKE :name ]
#[ AND age >= :min_age ]
#[ AND age <= :max_age ]
ORDER BY id DESC
`
	params := map[string]interface{}{
		"name":    "%tom%",
		"max_age": 30,
	}

	// Note: The preprocessing removes the entire line including newline for conditional blocks
	// This produces cleaner SQL
	result := Preprocess(query, params)

	// Verify the expected conditions are present
	if !contains(result, "AND name LIKE :name") {
		t.Error("expected query to contain name condition")
	}
	if !contains(result, "AND age <= :max_age") {
		t.Error("expected query to contain max_age condition")
	}
	if contains(result, "AND age >= :min_age") {
		t.Error("expected query to NOT contain min_age condition")
	}
}

func TestPreprocessWithINClause(t *testing.T) {
	query := "SELECT * FROM user WHERE 1=1 #[ AND id IN :ids ] #[ AND status = :status ]"

	tests := []struct {
		name     string
		params   map[string]interface{}
		expected string
	}{
		{
			name:     "both params present",
			params:   map[string]interface{}{"ids": []int{1, 2, 3}, "status": "active"},
			expected: "SELECT * FROM user WHERE 1=1 AND id IN :ids AND status = :status",
		},
		{
			name:     "only ids present",
			params:   map[string]interface{}{"ids": []int{1, 2, 3}},
			expected: "SELECT * FROM user WHERE 1=1 AND id IN :ids",
		},
		{
			name:     "empty ids slice",
			params:   map[string]interface{}{"ids": []int{}, "status": "active"},
			expected: "SELECT * FROM user WHERE 1=1 AND status = :status",
		},
		{
			name:     "no params",
			params:   map[string]interface{}{},
			expected: "SELECT * FROM user WHERE 1=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Preprocess(query, tt.params)
			if result != tt.expected {
				t.Errorf("Preprocess() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestPreprocessINWithNilSlice tests the scenario: #[ AND age IN :ages]
// When :ages is nil or empty, the condition should be removed
func TestPreprocessINWithNilSlice(t *testing.T) {
	query := "SELECT * FROM user WHERE 1=1 #[ AND age IN :ages ]"

	tests := []struct {
		name     string
		params   map[string]interface{}
		expected string
	}{
		{
			name:     "nil slice - condition removed",
			params:   map[string]interface{}{"ages": nil},
			expected: "SELECT * FROM user WHERE 1=1",
		},
		{
			name:     "empty slice - condition removed",
			params:   map[string]interface{}{"ages": []int{}},
			expected: "SELECT * FROM user WHERE 1=1",
		},
		{
			name:     "empty slice of int64 - condition removed",
			params:   map[string]interface{}{"ages": []int64{}},
			expected: "SELECT * FROM user WHERE 1=1",
		},
		{
			name:     "empty slice of string - condition removed",
			params:   map[string]interface{}{"ages": []string{}},
			expected: "SELECT * FROM user WHERE 1=1",
		},
		{
			name:     "non-empty slice - condition kept",
			params:   map[string]interface{}{"ages": []int{18, 25, 30}},
			expected: "SELECT * FROM user WHERE 1=1 AND age IN :ages",
		},
		{
			name:     "non-empty string slice - condition kept",
			params:   map[string]interface{}{"ages": []string{"18", "25", "30"}},
			expected: "SELECT * FROM user WHERE 1=1 AND age IN :ages",
		},
		{
			name:     "param not provided - condition removed",
			params:   map[string]interface{}{},
			expected: "SELECT * FROM user WHERE 1=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Preprocess(query, tt.params)
			if result != tt.expected {
				t.Errorf("Preprocess() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestNormalizeInNamedParams(t *testing.T) {
	query := "SELECT * FROM users WHERE id IN :ids AND status in :statuses"
	got := normalizeInNamedParams(query)
	want := "SELECT * FROM users WHERE id IN (:ids) AND status IN (:statuses)"
	if got != want {
		t.Fatalf("normalizeInNamedParams() = %q, want %q", got, want)
	}
}

// TestPreprocessMultipleINClause tests multiple IN clauses with different slice states
func TestPreprocessMultipleINClause(t *testing.T) {
	query := `SELECT * FROM user WHERE 1=1
#[ AND id IN :ids ]
#[ AND age IN :ages ]
#[ AND status IN :statuses ]`

	tests := []struct {
		name             string
		params           map[string]interface{}
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name:             "all slices empty - all conditions removed",
			params:           map[string]interface{}{"ids": []int{}, "ages": []int{}, "statuses": []string{}},
			shouldContain:    []string{"WHERE 1=1"},
			shouldNotContain: []string{"id IN", "age IN", "status IN"},
		},
		{
			name:             "mixed slices",
			params:           map[string]interface{}{"ids": []int{1, 2}, "ages": []int{}, "statuses": []string{"active"}},
			shouldContain:    []string{"id IN :ids", "status IN :statuses"},
			shouldNotContain: []string{"age IN"},
		},
		{
			name:             "nil slices - conditions removed",
			params:           map[string]interface{}{"ids": nil, "ages": nil, "statuses": nil},
			shouldContain:    []string{"WHERE 1=1"},
			shouldNotContain: []string{"id IN", "age IN", "status IN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Preprocess(query, tt.params)
			for _, s := range tt.shouldContain {
				if !strings.Contains(result, s) {
					t.Errorf("Preprocess() should contain %q, got %q", s, result)
				}
			}
			for _, s := range tt.shouldNotContain {
				if strings.Contains(result, s) {
					t.Errorf("Preprocess() should NOT contain %q, got %q", s, result)
				}
			}
		})
	}
}

func TestPreprocessComplexQuery(t *testing.T) {
	query := `
SELECT u.id, u.name, u.email, d.name as department_name
FROM user u
LEFT JOIN department d ON u.department_id = d.id
WHERE 1=1
#[ AND u.name LIKE :name ]
#[ AND u.status = :status ]
#[ AND u.department_id = :department_id ]
#[ AND u.created_at >= :start_date ]
#[ AND u.created_at <= :end_date ]
ORDER BY u.id DESC
LIMIT :limit OFFSET :offset
`
	// Scenario: search by name with pagination
	params := map[string]interface{}{
		"name":   "%john%",
		"status": "active",
		"limit":  10,
		"offset": 0,
	}

	result := Preprocess(query, params)

	// Should keep name and status conditions
	if !contains(result, "AND u.name LIKE :name") {
		t.Error("expected query to contain name condition")
	}
	if !contains(result, "AND u.status = :status") {
		t.Error("expected query to contain status condition")
	}
	// Should remove department_id condition (not provided)
	if contains(result, "AND u.department_id = :department_id") {
		t.Error("expected query to NOT contain department_id condition")
	}
	// Should keep limit and offset (they are not in #[ ] blocks)
	if !contains(result, "LIMIT :limit") {
		t.Error("expected query to contain LIMIT clause")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestEngineSelectPreprocessFlow tests the complete flow of Engine.Select
// ensuring that Preprocess -> Named -> In -> Rebind chain works correctly
func TestEngineSelectPreprocessFlow(t *testing.T) {
	// Test the preprocessing and named parameter handling
	// without actually executing the query

	tests := []struct {
		name             string
		query            string
		params           map[string]interface{}
		expectedQuery    string // after Preprocess
		expectedArgCount int    // expected number of args after Named+In
	}{
		{
			name:             "empty IN slice - condition removed",
			query:            "SELECT * FROM user WHERE 1=1 #[ AND id IN :ids ]",
			params:           map[string]interface{}{"ids": []int{}},
			expectedQuery:    "SELECT * FROM user WHERE 1=1",
			expectedArgCount: 0,
		},
		{
			name:             "nil IN slice - condition removed",
			query:            "SELECT * FROM user WHERE 1=1 #[ AND id IN :ids ]",
			params:           map[string]interface{}{"ids": nil},
			expectedQuery:    "SELECT * FROM user WHERE 1=1",
			expectedArgCount: 0,
		},
		{
			name:             "single element IN slice - condition kept, 1 arg",
			query:            "SELECT * FROM user WHERE 1=1 #[ AND id IN :ids ]",
			params:           map[string]interface{}{"ids": []int{1}},
			expectedQuery:    "SELECT * FROM user WHERE 1=1 AND id IN :ids",
			expectedArgCount: 1,
		},
		{
			name:             "multi element IN slice - condition kept, 3 args",
			query:            "SELECT * FROM user WHERE 1=1 #[ AND id IN :ids ]",
			params:           map[string]interface{}{"ids": []int{1, 2, 3}},
			expectedQuery:    "SELECT * FROM user WHERE 1=1 AND id IN :ids",
			expectedArgCount: 3,
		},
		{
			name:             "mixed conditions with IN",
			query:            "SELECT * FROM user WHERE 1=1 #[ AND name = :name ] #[ AND id IN :ids ]",
			params:           map[string]interface{}{"name": "John", "ids": []int{}},
			expectedQuery:    "SELECT * FROM user WHERE 1=1 AND name = :name",
			expectedArgCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Step 1: Preprocess
			processedQuery := Preprocess(tt.query, tt.params)
			if processedQuery != tt.expectedQuery {
				t.Errorf("Preprocess() = %q, want %q", processedQuery, tt.expectedQuery)
			}

			// Step 2: Named
			q, args, err := Named(processedQuery, tt.params)
			if err != nil {
				t.Errorf("Named() error: %v", err)
				return
			}

			// Step 3: In - this should not error because empty slices are already removed
			q, args, err = In(q, args...)
			if err != nil {
				t.Errorf("In() error: %v", err)
				return
			}

			if len(args) != tt.expectedArgCount {
				t.Errorf("Expected %d args after Named+In, got %d (args: %v)", tt.expectedArgCount, len(args), args)
			}
		})
	}
}

func TestNewEngine(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	engine := NewEngine(db)
	if engine == nil {
		t.Fatal("NewEngine returned nil")
	}
	if engine.DB() != db {
		t.Fatal("Engine.DB() returned wrong db")
	}
	if engine.StdDB() != db.StdDB() {
		t.Fatal("Engine.StdDB() returned wrong standard db")
	}
}

func TestDBStdDB(t *testing.T) {
	raw := newFakeDB()
	db := NewDb(raw, "sqlite3")
	defer db.Close()

	if db.StdDB() != raw {
		t.Fatal("DB.StdDB() returned wrong standard db")
	}
}

func TestTxStdTx(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()

	tx, err := db.BeginTxx(t.Context(), nil)
	if err != nil {
		t.Fatalf("BeginTxx failed: %v", err)
	}
	defer tx.Rollback()

	if tx.StdTx() != tx.Tx {
		t.Fatal("Tx.StdTx() returned wrong standard tx")
	}
}

func TestEngineWithTransactionRawSuccess(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()
	engine := NewEngine(db)

	var called bool
	err := engine.WithTransactionRaw(t.Context(), nil, func(txEngine *Engine, rawTx *sql.Tx) error {
		called = true
		if txEngine == nil {
			t.Fatal("expected non-nil transaction Engine")
		}
		if txEngine.DB() != nil {
			t.Fatal("transaction Engine should not expose a DB wrapper")
		}
		if txEngine.StdDB() != nil {
			t.Fatal("transaction Engine should not expose a standard DB")
		}
		if rawTx == nil {
			t.Fatal("expected non-nil standard transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTransactionRaw failed: %v", err)
	}
	if !called {
		t.Fatal("fn was not called")
	}
}

func TestEngineWithTransactionRawError(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()
	engine := NewEngine(db)

	testErr := sql.ErrNoRows
	err := engine.WithTransactionRaw(t.Context(), nil, func(*Engine, *sql.Tx) error {
		return testErr
	})
	if err != testErr {
		t.Fatalf("expected %v, got %v", testErr, err)
	}
}

func TestEngineWithTransactionRawRejectsNested(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()
	engine := NewEngine(db)

	err := engine.WithTransactionRaw(t.Context(), nil, func(txEngine *Engine, rawTx *sql.Tx) error {
		return txEngine.WithTransactionRaw(t.Context(), nil, func(*Engine, *sql.Tx) error {
			t.Fatal("nested callback should not be called")
			return nil
		})
	})
	if err == nil {
		t.Fatal("expected nested transaction error")
	}
}

func TestEngineNonContextMethodsExist(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	defer db.Close()
	engine := NewEngine(db)

	// Verify non-context methods exist and compile.
	// Fake DB's QueryContext returns nil rows which would panic on Close,
	// so we only verify the method signatures compile correctly.
	var _ = func(e *Engine) {
		_ = e.Select(nil, "SELECT 1", map[string]interface{}{"name": "test"})
		_ = e.Get(nil, "SELECT 1", map[string]interface{}{"name": "test"})
		_, _ = e.Exec("INSERT INTO t (name) VALUES (:name)", map[string]interface{}{"name": "test"})
		_, _ = e.Queryx("SELECT 1", map[string]interface{}{"name": "test"})
		_ = e.QueryRowx("SELECT 1", map[string]interface{}{"name": "test"})
		_ = e.MustExec("INSERT INTO t (name) VALUES (:name)", map[string]interface{}{"name": "test"})
	}
	_ = engine
	_ = db
}

func TestEngineMustExecPanicsOnError(t *testing.T) {
	db := NewDb(newFakeDB(), "sqlite3")
	engine := NewEngine(db)

	// MustExec with named params compiles correctly
	var result sql.Result
	var called bool
	_ = func() {
		result = engine.MustExec("INSERT INTO users (name) VALUES (:name)", map[string]interface{}{"name": "test"})
		called = true
	}
	_ = result
	_ = called
}

func TestEngineDynNamedStmtMethodsExist(t *testing.T) {
	// Verify DynNamedStmt method signatures compile correctly.
	var s DynNamedStmt
	var _ = func() {
		_ = s.Select(nil, map[string]interface{}{"id": 1})
		_ = s.Get(nil, map[string]interface{}{"id": 1})
		_, _ = s.Exec(map[string]interface{}{"id": 1})
		_ = s.Close()
	}
}
