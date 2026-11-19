package store

import (
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

// IsDuplicateKey checks if a key has been duplicated
func IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}

	if myErr, ok := errors.AsType[*mysql.MySQLError](err); ok {
		return myErr.Number == 1062
	}

	if pqErr, ok := errors.AsType[*pq.Error](err); ok {
		return pqErr.Code == "23505"
	}

	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
