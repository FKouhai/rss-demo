package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

func TestIsDuplicateKey(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"mysql duplicate", &mysql.MySQLError{Number: 1062}, true},
		{"mysql other", &mysql.MySQLError{Number: 1146}, false},
		{"mysql duplicate wrapped", fmt.Errorf("insert: %w", &mysql.MySQLError{Number: 1062}), true},
		{"postgres unique violation", &pq.Error{Code: "23505"}, true},
		{"postgres other", &pq.Error{Code: "42P01"}, false},
		{"postgres wrapped", fmt.Errorf("exec: %w", &pq.Error{Code: "23505"}), true},
		{"sqlite message", errors.New("UNIQUE constraint failed: outbox.id"), true},
		{"sqlite text with digits still sqlite", errors.New("UNIQUE constraint failed: 1062"), true},
		{"other error", errors.New("connection refused"), false},
		{"number alone is not a duplicate", errors.New("got 1062 rows"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsDuplicateKey(tc.err); got != tc.want {
				t.Errorf("IsDuplicateKey(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
