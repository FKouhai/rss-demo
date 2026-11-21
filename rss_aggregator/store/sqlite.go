package store

import _ "embed"

import (
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
	return &aggregatorStore{db: conn, driver: db.DriverSQLite}, nil
}
