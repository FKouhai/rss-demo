package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/fstest"

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

func TestMigrate_AppliesInOrderAndIdempotent(t *testing.T) {
	fsys := fstest.MapFS{
		"001_init.sql": {Data: []byte(`CREATE TABLE t (id TEXT PRIMARY KEY, v TEXT);
CREATE INDEX idx_t_v ON t(v);`)},
		"002_add.sql": {Data: []byte(`ALTER TABLE t ADD COLUMN w TEXT;
CREATE INDEX idx_t_w ON t(w);`)},
		"notes.txt": {Data: []byte("ignored")},
	}
	db, err := sql.Open(string(DriverSQLite), ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := Migrate(ctx, db, DriverSQLite, fsys, "."); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Second run is a no-op.
	if err := Migrate(ctx, db, DriverSQLite, fsys, "."); err != nil {
		t.Fatalf("Migrate again: %v", err)
	}
	var versions string
	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		versions += v + ","
	}
	if versions != "001_init.sql,002_add.sql," {
		t.Errorf("versions = %q, want both in order", versions)
	}
	// New column usable (proves 002 ran, not just recorded).
	if _, err := db.Exec(`INSERT INTO t (id, v, w) VALUES ('a', 'b', 'c')`); err != nil {
		t.Errorf("insert with new column: %v", err)
	}
}
