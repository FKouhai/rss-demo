package store

import (
	_ "github.com/go-sql-driver/mysql"

	db "github.com/FKouhai/rss-demo/libs/db"
)

// newMySQLStore builds a Store backed by MySQL. The DSN encodes the
// client connection (host, credentials, database).
func newMySQLStore(dsn string) (Store, error) {
	conn, err := db.OpenMigrated(db.DriverMySQL, dsn, migrationsFS, "migrations/mysql")
	if err != nil {
		return nil, err
	}
	return &outboxStore{db: conn, driver: db.DriverMySQL}, nil
}
