// Package db is the shared SQL engine toolkit for Go services.
//
// It owns everything that is identical across services: the supported
// drivers, open-and-migrate, per-engine placeholder rendering, and
// duplicate-key classification. What it deliberately does NOT own is any
// table, row type, or query — each service keeps its own domain store
// (e.g. rss_outbox/store) built on top of these primitives.
package db

import (
	"database/sql"
	"fmt"
	"strconv"
)

// Driver is a supported SQL backend.
type Driver string

const (
	DriverSQLite   Driver = "sqlite"
	DriverMySQL    Driver = "mysql"
	DriverPostgres Driver = "postgres"
)

// Open connects to dsn and runs schema as migrate-on-start DDL, returning
// a ready *sql.DB. Callers own the schema (usually go:embed'ed next to
// their dialect constructor).
func Open(d Driver, dsn, schema string) (*sql.DB, error) {
	conn, err := sql.Open(string(d), dsn)
	if err != nil {
		return nil, fmt.Errorf("%s open: %w", d, err)
	}
	if _, err := conn.Exec(schema); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s migrate: %w", d, err)
	}
	return conn, nil
}

// Placeholder renders a single positional placeholder: "?" for SQLite and
// MySQL (1-based n ignored), "$N" for PostgreSQL.
func Placeholder(d Driver, n int) string {
	if d == DriverPostgres {
		return "$" + strconv.Itoa(n)
	}
	return "?"
}

// Placeholders renders a comma-separated placeholder list starting at
// 1-based start, e.g. "?, ?, ?" or "$1, $2, $3".
func Placeholders(d Driver, start, total int) string {
	parts := make([]string, total)
	for i := range parts {
		parts[i] = Placeholder(d, start+i)
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}
