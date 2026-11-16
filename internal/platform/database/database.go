package database

import (
	"database/sql"

	"binasiswa/backend/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func Open(cfg config.DatabaseConfig) (*sql.DB, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	db, err := sql.Open("pgx", cfg.URL)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.MaxOpenConnections)
	db.SetMaxIdleConns(cfg.MaxIdleConnections)
	db.SetConnMaxLifetime(cfg.ConnectionLifetime)
	db.SetConnMaxIdleTime(cfg.ConnectionIdleTime)

	return db, nil
}
