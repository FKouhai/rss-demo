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
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
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
	if _, err := conn.Exec(schema); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s migrate: %w", d, err)
	}
	return conn, nil
}

// OpenMigrated dials the backend (same mkdir/sqlite-pooling/params as
// Open) and applies the pending versioned migration files from fsys/dir
// in lexical order, tracking them in schema_migrations. There is no down
// path by design: roll forward, never back.
func OpenMigrated(d Driver, dsn string, fsys fs.FS, dir string) (*sql.DB, error) {
	dialDSN := dsn
	if d == DriverSQLite && dialDSN != ":memory:" && !strings.Contains(dialDSN, "?") {
		if dir := filepath.Dir(dialDSN); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("%s mkdir %s: %w", d, dir, err)
			}
		}
	}
	if d == DriverMySQL {
		dialDSN = ensureMySQLParams(dialDSN)
	}
	conn, err := sql.Open(string(d), dialDSN)
	if err != nil {
		return nil, fmt.Errorf("%s open: %w", d, err)
	}
	if d == DriverSQLite {
		conn.SetMaxOpenConns(1)
	}
	if err := Migrate(context.Background(), conn, d, fsys, dir); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// // Migrate applies pending *.sql files from fsys/dir in lexical order
// (001_*.sql before 002_*.sql), recording each in schema_migrations.
// Files run whole (multi-statement OK: mysql gets multiStatements via
// Open, pg/sqlite run multi-statement Exec natively). Re-running is a
// no-op: applied versions are skipped, so concurrent boots racing the
// same file converge (worst case both run idempotent DDL).
//
// Already-applied DDL is forgiven, not fatal: an Exec error naming a
// duplicate/already-existing object records the version and continues.
// This covers databases migrated by hand (or a previous scheme) before
// the version row existed. Migrations must still be written
// idempotently (IF NOT EXISTS where the dialect allows); the
// forgiveness is a backstop, not the mechanism.
func Migrate(ctx context.Context, conn *sql.DB, d Driver, fsys fs.FS, dir string) error {
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("%s migrate: version table: %w", d, err)
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("%s migrate: read %s: %w", d, dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var n int
		q := `SELECT COUNT(*) FROM schema_migrations WHERE version = ` + Placeholder(d, 1)
		if err := conn.QueryRowContext(ctx, q, name).Scan(&n); err != nil {
			return fmt.Errorf("%s migrate: check %s: %w", d, name, err)
		}
		if n > 0 {
			continue
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return fmt.Errorf("%s migrate: read %s: %w", d, name, err)
		}
		if _, err := conn.ExecContext(ctx, string(body)); err != nil {
			if isAlreadyApplied(err) {
				// Converged by hand or by a racing boot: record and move on.
			} else {
				return fmt.Errorf("%s migrate: apply %s: %w", d, name, err)
			}
		}
		ins := `INSERT INTO schema_migrations (version) VALUES (` + Placeholder(d, 1) + `)`
		if _, err := conn.ExecContext(ctx, ins, name); err != nil {
			return fmt.Errorf("%s migrate: record %s: %w", d, name, err)
		}
	}
	return nil
}

// isAlreadyApplied reports whether err only means the DDL already
// holds: sqlite "duplicate column name", postgres "... already exists",
// mysql "Duplicate ...". Case-insensitive contains on purpose: driver
// messages vary by version, and the consequence of a false positive is
// just a recorded version for DDL that would be a no-op anyway.
func isAlreadyApplied(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "already exists")
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
