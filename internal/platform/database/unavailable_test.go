package database

import (
	"errors"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

func TestIsUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "lost server connection", err: &mysql.MySQLError{Number: 2013}, want: true},
		{name: "bad credentials", err: &mysql.MySQLError{Number: 1045}, want: false},
		{name: "query error", err: errors.New("invalid query"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsUnavailable(test.err); got != test.want {
				t.Fatalf("IsUnavailable() = %t, want %t", got, test.want)
			}
		})
	}
}
