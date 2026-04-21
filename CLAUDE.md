# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build, Test, and Lint Commands

```bash
# Run tests
go test -v -count=1 ./...

# Run tests with race detector
go test -v -race -count=1 ./...

# Run tests for a specific package
go test -v -count=1 ./reflectx

# Run a specific test
go test -v -run TestFunctionName ./...

# Install linting tools
make tooling

# Run linters (requires tooling installed)
make lint

# Run vulnerability check
make vuln-check

# Format code
make fmt
```

## Architecture Overview

sqlx is a Go library that extends the standard `database/sql` package. It wraps the standard types (DB, Tx, Stmt, Row, Rows) with enhanced versions that add struct scanning, named parameters, and other conveniences.

### Core Files

- **`sqlx.go`** - Main wrapper types:
  - `DB`, `Tx`, `Stmt`, `Conn` - Wrappers around sql.DB/Tx/Stmt/Conn
  - `Row`, `Rows` - Enhanced row types with StructScan, MapScan, SliceScan
  - Key functions: `Select()`, `Get()`, `MustExec()`, `StructScan()`

- **`bind.go`** - Bind variable handling for different database drivers:
  - `QUESTION` (MySQL, SQLite): `?`
  - `DOLLAR` (PostgreSQL): `$1, $2, ...`
  - `NAMED` (Oracle): `:name`
  - `AT` (SQL Server, Azure SQL): `@p1, @p2, ...`
  - `In()` - Expands slice values for `IN` clauses

- **`named.go`** - Named parameter support:
  - `NamedQuery()`, `NamedExec()` - Execute queries with named parameters
  - `NamedStmt` - Prepared statement with named parameters
  - Supports structs (via `db` tags), maps, and slices for batch inserts

- **`sqlx_context.go`** - Context-aware versions of all methods (Go 1.8+)

- **`reflectx/reflect.go`** - Reflection utilities:
  - `Mapper` - Maps struct fields to column names using `db` tags
  - Caches field mappings for performance
  - Handles embedded structs

- **`types/types.go`** - Custom types implementing `driver.Valuer` and `sql.Scanner`:
  - `JSONText` - JSON data stored as []byte
  - `GzippedText` - Automatically compressed text
  - `BitBool` - MySQL BIT(1) boolean representation

### Key Patterns

**Struct Scanning**: Fields are matched to columns using the `db` struct tag. Without a tag, field names are lowercased:
```go
type Person struct {
    FirstName string `db:"first_name"` // maps to "first_name" column
    Email     string                   // maps to "email" column (lowercased)
}
```

**Unsafe Mode**: Call `.Unsafe()` on DB/Tx/Stmt to silently ignore missing columns instead of returning an error.

**NameMapper**: The global `NameMapper` function controls field-to-column name mapping. Default is `strings.ToLower`. Changes must be made before any struct scanning occurs as mappings are cached.

## Backwards Compatibility

Supports the most recent two Go versions. Breaking API changes require a major version bump via Go modules.

## Database Drivers for Testing

Tests require running database instances. The test files use:
- PostgreSQL: `github.com/lib/pq`
- MySQL: `github.com/go-sql-driver/mysql`
- SQLite: `github.com/mattn/go-sqlite3`
