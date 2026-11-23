package store

import (
	_ "github.com/lib/pq"

	db "github.com/FKouhai/rss-demo/libs/db"
)

// newPostgresStore builds a Store backed by PostgreSQL. The DSN encodes
// the client connection; PG uses numbered ($1) placeholders.
func newPostgresStore(dsn string) (Store, error) {
	conn, err := db.OpenMigrated(db.DriverPostgres, dsn, migrationsFS, "migrations/postgres")
	if err != nil {
		return nil, err
	}
	return &outboxStore{db: conn, driver: db.DriverPostgres}, nil
}
