package store

import _ "embed"

import (
	"context"

	_ "github.com/lib/pq"

	db "github.com/FKouhai/rss-demo/libs/db"
)

//go:embed postgres/schema.sql
var postgresSchema string

// newPostgresStore builds a Store backed by PostgreSQL. The DSN encodes
// the client connection; PG uses numbered ($1) placeholders.
func newPostgresStore(dsn string) (Store, error) {
	conn, err := db.Open(db.DriverPostgres, dsn, postgresSchema)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	for _, col := range []struct{ name, typ string }{
		{"attempts", "INTEGER NOT NULL DEFAULT 0"},
		{"next_retry_at", "TIMESTAMPTZ"},
	} {
		if err := db.EnsureColumn(ctx, conn, db.DriverPostgres, "outbox", col.name, col.typ); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return &outboxStore{db: conn, driver: db.DriverPostgres}, nil
}
