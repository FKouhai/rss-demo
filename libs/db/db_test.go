package db

import (
	"context"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenSQLiteMemory(t *testing.T) {
	conn, err := Open(DriverSQLite, ":memory:", `CREATE TABLE t (id TEXT PRIMARY KEY, v TEXT)`)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// :memory: databases are per-connection; pin the pool to one so all
	// statements see the same database.
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, "INSERT INTO t (id, v) VALUES ("+Placeholders(DriverSQLite, 1, 2)+")", "a", "b"); err != nil {
		t.Fatalf("insert with placeholders: %v", err)
	}
	var got string
	if err := conn.QueryRowContext(ctx, "SELECT v FROM t WHERE id = 'a'").Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if got != "b" {
		t.Errorf("got %q, want b", got)
	}
}

func TestOpenRunsSchema(t *testing.T) {
	// A second Open against the same file must tolerate an existing schema
	// (migrate-on-start is idempotent via IF NOT EXISTS).
	dir := t.TempDir()
	dsn := dir + "/test.db"
	schema := `CREATE TABLE IF NOT EXISTS t (id TEXT PRIMARY KEY)`
	for range 2 {
		conn, err := Open(DriverSQLite, dsn, schema)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		_ = conn.Close()
	}
}

func TestPlaceholder(t *testing.T) {
	if got := Placeholder(DriverSQLite, 3); got != "?" {
		t.Errorf("sqlite placeholder = %q, want ?", got)
	}
	if got := Placeholder(DriverMySQL, 3); got != "?" {
		t.Errorf("mysql placeholder = %q, want ?", got)
	}
	if got := Placeholder(DriverPostgres, 3); got != "$3" {
		t.Errorf("postgres placeholder = %q, want $3", got)
	}
	if got := Placeholder(Driver("unknown"), 1); got != "?" {
		t.Errorf("unknown driver placeholder = %q, want ? fallback", got)
	}
}

func TestPlaceholders(t *testing.T) {
	if got := Placeholders(DriverSQLite, 1, 3); got != "?, ?, ?" {
		t.Errorf("sqlite = %q", got)
	}
	if got := Placeholders(DriverMySQL, 1, 2); got != "?, ?" {
		t.Errorf("mysql = %q", got)
	}
	if got := Placeholders(DriverPostgres, 1, 3); got != "$1, $2, $3" {
		t.Errorf("postgres = %q", got)
	}
	if got := Placeholders(DriverPostgres, 5, 3); got != "$5, $6, $7" {
		t.Errorf("postgres offset = %q", got)
	}
}

func TestIdent(t *testing.T) {
	if got := Ident(DriverMySQL, "key"); got != "`key`" {
		t.Errorf("mysql = %q, want backticks (KEY is reserved)", got)
	}
	if got := Ident(DriverPostgres, "key"); got != `"key"` {
		t.Errorf("postgres = %q, want double quotes", got)
	}
	if got := Ident(DriverSQLite, "key"); got != "key" {
		t.Errorf("sqlite = %q, want bare", got)
	}
}

func TestEnsureMySQLParams(t *testing.T) {
	base := "user:pw@tcp(127.0.0.1:3306)/db"
	got := ensureMySQLParams(base)
	for _, want := range []string{"multiStatements=true", "parseTime=true", "loc=UTC"} {
		if !strings.Contains(got, want) {
			t.Errorf("dsn = %q, want %q", got, want)
		}
	}
	// Existing params are preserved, never duplicated.
	withParams := base + "?parseTime=true&charset=utf8mb4"
	got = ensureMySQLParams(withParams)
	if c := strings.Count(got, "parseTime=true"); c != 1 {
		t.Errorf("dsn = %q, parseTime duplicated %d times", got, c)
	}
	if !strings.Contains(got, "charset=utf8mb4") || !strings.Contains(got, "multiStatements=true") {
		t.Errorf("dsn = %q, want existing kept and missing added", got)
	}
}
