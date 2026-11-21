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
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
// their dialect constructor). For file-backed sqlite DSNs the parent
// directory is created as needed, so services can point at paths (e.g.
// /var/lib/<svc>/app.db) that don't exist in minimal container images.
// Sqlite gets a single pooled connection: these services are low-write
// and concurrent writers otherwise hit SQLITE_BUSY, whose redelivery
// fallout (exhausted MaxDeliver → rows stuck pending forever) is far
// worse than serialized access.
func Open(d Driver, dsn, schema string) (*sql.DB, error) {
	if d == DriverSQLite && dsn != ":memory:" && !strings.Contains(dsn, "?") {
		if dir := filepath.Dir(dsn); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("%s mkdir %s: %w", d, dir, err)
			}
		}
	}
	conn, err := sql.Open(string(d), dsn)
	if err != nil {
		return nil, fmt.Errorf("%s open: %w", d, err)
	}
	if d == DriverSQLite {
		conn.SetMaxOpenConns(1)
	}
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
