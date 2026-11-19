package store

import _ "embed"

import _ "github.com/lib/pq"

//go:embed postgres/schema.sql
var postgresSchema string

// newPostgresStore builds a Store backed by PostgreSQL. The DSN encodes
// the client connection; PG uses numbered ($1) placeholders.
func newPostgresStore(dsn string) (Store, error) {
	return newOutboxStore(dialect{
		driver:      "postgres",
		schema:      postgresSchema,
		placeholder: dollar,
	}, dsn)
}
