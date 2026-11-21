package store

import _ "embed"

import (
	_ "github.com/lib/pq"

	db "github.com/FKouhai/rss-demo/libs/db"
)

//go:embed postgres/schema.sql
var postgresSchema string

// newPostgresStore builds a Store backed by PostgreSQL.
func newPostgresStore(dsn string) (Store, error) {
	conn, err := db.Open(db.DriverPostgres, dsn, postgresSchema)
	if err != nil {
		return nil, err
	}
	return &configStore{db: conn, driver: db.DriverPostgres}, nil
}
