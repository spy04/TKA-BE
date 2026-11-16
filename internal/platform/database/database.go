package database

import (
	"database/sql"
	"time"

	"binasiswa/backend/internal/config"
	mysql "github.com/go-sql-driver/mysql"
)

func Open(cfg config.DatabaseConfig) (*sql.DB, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	driverConfig := mysql.Config{
		User:                 cfg.User,
		Passwd:               cfg.Password,
		Net:                  "tcp",
		Addr:                 cfg.TCPAddress(),
		DBName:               cfg.Name,
		ParseTime:            true,
		Loc:                  time.UTC,
		Collation:            "utf8mb4_unicode_ci",
		AllowNativePasswords: true,
		Params: map[string]string{
			"charset":   "utf8mb4",
			"time_zone": "'+00:00'",
		},
	}

	db, err := sql.Open("mysql", driverConfig.FormatDSN())
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.MaxOpenConnections)
	db.SetMaxIdleConns(cfg.MaxIdleConnections)
	db.SetConnMaxLifetime(cfg.ConnectionLifetime)
	db.SetConnMaxIdleTime(cfg.ConnectionIdleTime)

	return db, nil
}
