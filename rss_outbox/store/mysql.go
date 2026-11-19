package store

import _ "embed"

import _ "github.com/go-sql-driver/mysql"

//go:embed mysql/schema.sql
var mysqlSchema string

// newMySQLStore builds a Store backed by MySQL. The DSN encodes the
// client connection (host, credentials, database).
func newMySQLStore(dsn string) (Store, error) {
	return newOutboxStore(dialect{
		driver:      "mysql",
		schema:      mysqlSchema,
		placeholder: qmark,
	}, dsn)
}
