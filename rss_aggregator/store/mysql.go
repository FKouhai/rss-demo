package store

import _ "embed"

import (
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
	return &aggregatorStore{db: conn, driver: db.DriverMySQL}, nil
}
