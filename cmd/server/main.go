package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "golang-test-task/api"
	"golang-test-task/sqlc"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxConns          = 60
	minConns          = 10
	maxConnLifetime   = 120 * time.Second
	maxConnIdleTime   = 20 * time.Second
	healthCheckPeriod = 30 * time.Second
)

func main() {
	dsn := getEnv("POSTGRES_DSN", "")
	addr := getEnv("SERVER_ADDR", ":8080")

	if dsn == "" {
		slog.Error("POSTGRES_DSN is not set")
		return
	}

	pool, err := NewPostgresDB(dsn)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		return
	}
	defer pool.Close()

	slog.Info("Successfully connected to database")

	queries := sqlc.New(pool)

	server := NewServer(queries)

	strictHandler := api.NewStrictHandler(server, nil)

	handler := api.Handler(strictHandler)

	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	serverErrors := make(chan error, 1)

	go func() {
		slog.Info("Starting server", "address", addr)
		serverErrors <- srv.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			return
		}
	case sig := <-shutdown:
		slog.Info("Received shutdown signal", "signal", sig.String())

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("Failed to shutdown server gracefully", "error", err)
			srv.Close()
		}

		slog.Info("Server stopped gracefully")
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func NewPostgresDB(dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}

	config.MaxConns = maxConns
	config.MinConns = minConns
	config.MaxConnLifetime = maxConnLifetime
	config.MaxConnIdleTime = maxConnIdleTime
	config.HealthCheckPeriod = healthCheckPeriod

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, err
	}

	if err = pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}
