# Logging Guidelines

> How logging is done in this project.

---

## Overview

**This project does not log.** It is a library that provides query helpers and struct scanning — it has no logging infrastructure, no log library dependency, and no log calls anywhere in the codebase.

---

## What This Means for Contributors

- **Do not add logging** to the library. Libraries should not make logging decisions for consumers.
- Return errors instead of logging them. Let the caller decide how to handle and record errors.
- If diagnostic output is needed during development, use the `testing` package's `t.Log()` inside tests.

---

## What Callers Should Know

Consumers of this library handle their own logging. Typical patterns:

- Log query errors after they're returned from `Select()`, `Get()`, `Exec()`, etc.
- Log database connection failures from `Open()` / `ConnectContext()`
- Wrap errors with context (query text, parameters) at the application layer — not inside sqlx

---

## No Sensitive Data Handling

Since this library does not log, there are no PII/secrets logging concerns at this layer. Consumers are responsible for ensuring they do not log raw query parameters containing sensitive data.
