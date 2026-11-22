package store

import _ "embed"

import (
	"context"

	_ "modernc.org/sqlite"

	db "github.com/FKouhai/rss-demo/libs/db"
)

//go:embed sqlite/schema.sql
var sqliteSchema string

// newSQLiteStore builds a Store backed by SQLite. With a DSN like
// ":memory:" or a temp file path it serves as the fast backend for
// integration tests; production points it at a persistent path.
func newSQLiteStore(dsn string) (Store, error) {
	conn, err := db.Open(db.DriverSQLite, dsn, sqliteSchema)
	if err != nil {
		return nil, err
	}
	// Evolve pre-retry databases: CREATE TABLE IF NOT EXISTS skips
	// existing tables, so new columns arrive via check-then-add.
	ctx := context.Background()
	for _, col := range []struct{ name, typ string }{
		{"attempts", "INTEGER NOT NULL DEFAULT 0"},
		{"next_retry_at", "DATETIME"},
	} {
		if err := db.EnsureColumn(ctx, conn, db.DriverSQLite, "outbox", col.name, col.typ); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return &outboxStore{db: conn, driver: db.DriverSQLite}, nil
}
