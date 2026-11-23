package store

import (
	_ "modernc.org/sqlite"

	db "github.com/FKouhai/rss-demo/libs/db"
)

// newSQLiteStore builds a Store backed by SQLite. With a DSN like
// ":memory:" or a temp file path it serves as the fast backend for
// integration tests; production points it at a persistent path.
func newSQLiteStore(dsn string) (Store, error) {
	conn, err := db.OpenMigrated(db.DriverSQLite, dsn, migrationsFS, "migrations/sqlite")
	if err != nil {
		return nil, err
	}
	return &outboxStore{db: conn, driver: db.DriverSQLite}, nil
}
