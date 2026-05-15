package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnIdleTime = 5 * time.Minute

	const maxAttempts = 10
	const delay = 3 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			err = pool.Ping(ctx)
		}
		cancel()

		if err == nil {
			slog.Info("DB OK: database connection established successfully!")
			return pool, nil
		}

		if attempt == maxAttempts {
			return nil, fmt.Errorf("DB ERR: database unavailable after %d attempts: %w", maxAttempts, err)
		}
		slog.Info("DB WAIT: waiting for database", "attempt", attempt, "err", err)
		time.Sleep(delay)
	}
	return nil, nil // unreachable
}
