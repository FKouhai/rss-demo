package store

import _ "embed"

import (
	"context"

	_ "github.com/go-sql-driver/mysql"

	db "github.com/FKouhai/rss-demo/libs/db"
)

//go:embed mysql/schema.sql
var mysqlSchema string

// newMySQLStore builds a Store backed by MySQL. The DSN encodes the
// client connection (host, credentials, database).
func newMySQLStore(dsn string) (Store, error) {
	conn, err := db.Open(db.DriverMySQL, dsn, mysqlSchema)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	for _, col := range []struct{ name, typ string }{
		{"attempts", "INT NOT NULL DEFAULT 0"},
		{"next_retry_at", "DATETIME"},
	} {
		if err := db.EnsureColumn(ctx, conn, db.DriverMySQL, "outbox", col.name, col.typ); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return &outboxStore{db: conn, driver: db.DriverMySQL}, nil
}
