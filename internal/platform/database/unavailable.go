package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"

	mysql "github.com/go-sql-driver/mysql"
)

func IsUnavailable(err error) bool {
	if errors.Is(err, sql.ErrConnDone) ||
		errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) {
		switch mysqlError.Number {
		case 1040, 1053, 2002, 2003, 2006, 2013, 2055:
			return true
		}
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}
