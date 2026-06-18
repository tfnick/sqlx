// Package sqlx provides an Engine-first SQL API on top of database/sql.
//
// New application code should create a Manager, get an Engine with GetEngine,
// MustEngine, Engine, or DefaultEngine, and call query, dynamic SQL, CRUD,
// batch, prepared, and transaction methods from that Engine.
//
// The lower-level database/sql wrapper types remain available to support the
// Engine implementation and migration of existing code, but they are not the
// standard client-facing API.
package sqlx
