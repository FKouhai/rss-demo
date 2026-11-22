// Package db is the shared SQL engine toolkit for Go services.
//
// It owns everything that is identical across services: the supported
// drivers, open-and-migrate, per-engine placeholder rendering, and
// duplicate-key classification. What it deliberately does NOT own is any
// table, row type, or query — each service keeps its own domain store
// (e.g. rss_outbox/store) built on top of these primitives.
package db

import (
	"context"
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
	if d == DriverMySQL {
		// go-sql-driver/mysql defaults break us three ways: multi-statement
		// migrate (schema has CREATE TABLE + CREATE INDEX), DATETIME
		// columns scanning into []byte instead of time.Time, and naive
		// datetimes in the server zone. Enforce the params centrally so
		// no service DSN has to remember them.
		dsn = ensureMySQLParams(dsn)
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

// Ident quotes an identifier per engine: backticks for MySQL (where
// words like KEY are fully reserved), double quotes for PostgreSQL,
// bare for SQLite. Call sites stay generic; only this function branches.
func Ident(d Driver, name string) string {
	switch d {
	case DriverMySQL:
		return "`" + name + "`"
	case DriverPostgres:
		return `"` + name + `"`
	default:
		return name
	}
}

// ensureMySQLParams appends the driver params the stores require when
// missing: multiStatements (multi-statement migrate), parseTime (DATETIME
// into time.Time), loc=UTC (tz-aware round trip).
func ensureMySQLParams(dsn string) string {
	for _, kv := range []struct{ key, val string }{
		{"multiStatements", "true"},
		{"parseTime", "true"},
		{"loc", "UTC"},
	} {
		if strings.Contains(dsn, kv.key+"=") {
			continue
		}
		if strings.Contains(dsn, "?") {
			dsn += "&" + kv.key + "=" + kv.val
		} else {
			dsn += "?" + kv.key + "=" + kv.val
		}
	}
	return dsn
}

// EnsureColumn adds table.name with the given engine-specific DDL type when
// missing, so migrate-on-start (CREATE TABLE IF NOT EXISTS) can evolve
// existing databases without a migration runner. The existence check is
// check-then-add per engine: pragma_table_info (sqlite),
// information_schema (postgres, mysql). table/name are internal constants
// at every call site (never user input), so Sprintf is safe here.
func EnsureColumn(ctx context.Context, conn *sql.DB, d Driver, table, name, ddlType string) error {
	var check string
	switch d {
	case DriverSQLite:
		check = fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = '%s'", table, name)
	case DriverPostgres:
		check = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ` + Placeholder(d, 1) + ` AND column_name = ` + Placeholder(d, 2)
	case DriverMySQL:
		check = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ` + Placeholder(d, 1) + ` AND column_name = ` + Placeholder(d, 2)
	default:
		return fmt.Errorf("unsupported driver: %s", d)
	}
	var n int
	args := []any{}
	if d != DriverSQLite {
		args = []any{table, name}
	}
	if err := conn.QueryRowContext(ctx, check, args...).Scan(&n); err != nil {
		return fmt.Errorf("%s ensure column %s.%s check: %w", d, table, name, err)
	}
	if n > 0 {
		return nil
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, name, ddlType)); err != nil {
		return fmt.Errorf("%s ensure column %s.%s add: %w", d, table, name, err)
	}
	return nil
}
