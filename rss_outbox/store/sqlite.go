package store

import _ "embed"

import _ "modernc.org/sqlite"

//go:embed sqlite/schema.sql
var sqliteSchema string

// newSQLiteStore builds a Store backed by SQLite. With a DSN like
// ":memory:" or a temp file path it serves as the fast backend for
// integration tests; production points it at a persistent path.
func newSQLiteStore(dsn string) (Store, error) {
	return newOutboxStore(dialect{
		driver:      "sqlite",
		schema:      sqliteSchema,
		placeholder: qmark,
	}, dsn)
}
