package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"binasiswa/backend/internal/config"
	"binasiswa/backend/internal/platform/database"
	httpapi "binasiswa/backend/internal/platform/http"
)

func main() {
	if err := run(); err != nil {
		slog.Error("backend stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logger = logger.With("environment", cfg.Environment)

	var db *sql.DB
	if cfg.Database.Enabled {
		db, err = database.Open(cfg.Database)
		if err != nil {
			return fmt.Errorf("configure MariaDB connection: %w", err)
		}
		defer db.Close()
	}

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           httpapi.NewRouter(logger, db, cfg),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "address", cfg.Address)
		serverErrors <- server.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("gracefully shut down HTTP server: %w", err)
		}
		logger.Info("HTTP server stopped")
		return nil
	}
}
