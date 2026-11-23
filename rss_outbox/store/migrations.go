package store

import "embed"

// migrationsFS holds the versioned migration files applied in order by
// libs/db Migrate. One directory per engine; filenames sort lexically
// (001_*.sql before 002_*.sql).
//
//go:embed migrations
var migrationsFS embed.FS
