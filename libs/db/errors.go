package db

import (
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

// IsDuplicateKey reports whether err is a unique-constraint violation,
// across all supported drivers. MySQL (1062) and Postgres (23505,
// SQLSTATE unique_violation) expose typed errors; SQLite is matched by
// message text ("UNIQUE constraint failed").
func IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}

	if dup, ok := errors.AsType[*mysql.MySQLError](err); ok {
		return dup.Number == 1062
	}

	if dup, ok := errors.AsType[*pq.Error](err); ok {
		return dup.Code == "23505"
	}

	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
