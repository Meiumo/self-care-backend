package migrate

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

func Run(ctx context.Context, db *pgxpool.Pool) error {
	slog.Info("DB WAIT: running migrations")
	if _, err := db.Exec(ctx, schema); err != nil {
		return fmt.Errorf("DB ERR: migration failed: %w", err)
	}
	slog.Info("DB OK: migrations complete!")
	return nil
}
