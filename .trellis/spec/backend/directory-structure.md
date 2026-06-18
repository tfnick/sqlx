# Directory Structure

> How backend code is organized in this project.

---

## Overview

This is a **Go library** (module: `github.com/tfnick/sqlx`) that extends `database/sql`. All core functionality lives in the root `sqlx` package. Two sub-packages provide supporting utilities.

---

## Directory Layout

```
.
├── sqlx.go                # Core wrapper types (DB, Tx, Stmt, Conn, Row, Rows)
├── bind.go                # Bind variable handling + In() expansion
├── named.go               # Named parameter support
├── sqlx_context.go        # Context-aware method variants (Go 1.8+)
├── named_context.go       # Named context methods
├── engine.go              # Dynamic SQL engine with conditional blocks
├── doc.go                 # Package documentation
├── go.mod                 # Module definition
├── makefile               # Build, lint, format, test targets
├── reflectx/              # Reflection utilities sub-package
│   └── reflect.go         # Mapper, FieldInfo, StructMap, tag parsing
└── types/                 # Custom database types sub-package
    └── types.go           # JSONText, GzippedText, BitBool, NullJSONText
```

**Test files** are co-located with source files, following Go convention:
- `sqlx_test.go`, `bind_test.go`, `named_test.go`, `sqlx_context_test.go`, etc.

---

## Module Organization

- **Root `sqlx` package**: All public API types and functions. There is no internal layering (no separate "routes/services/utils"). Everything is in one flat package.
- **`reflectx` sub-package**: Internal reflection machinery for struct field mapping. Used by the root package but also importable by consumers.
- **`types` sub-package**: Optional convenience types implementing `driver.Valuer` and `sql.Scanner`. Independent of the core.

---

## Naming Conventions

- **Source files**: lowercase, underscore-separated (`sqlx_context.go`, `named_context.go`)
- **Test files**: `<source>_test.go` suffix, co-located
- **Packages**: single word, lowercase (`sqlx`, `reflectx`, `types`)
- **Types**: PascalCase (`DB`, `Rows`, `NamedStmt`, `JSONText`)
- **Functions**: PascalCase for exported, camelCase for unexported
- **Variables**: Minimal package-level vars; singletons use `sync.Mutex` or `sync.Map`

---

## Examples

- Well-organized reference: `sqlx.go:243-396` — the `DB` wrapper struct and its methods show the canonical pattern of embedding `*sql.DB` and delegating to it.
- Sub-package example: `reflectx/reflect.go` — a focused, single-file sub-package exposing `Mapper` as its primary type.
