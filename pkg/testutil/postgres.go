package testutil

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/romangolovachev/selfcare/internal/migrate"
	"github.com/romangolovachev/selfcare/pkg/database"
)

// StartPostgres spins up a PostgreSQL container, runs migrations, and returns
// a ready pool. The container is terminated automatically via t.Cleanup.
func StartPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("selfcare_test"),
		tcpostgres.WithUsername("selfcare"),
		tcpostgres.WithPassword("testpassword"),
	)
	if err != nil {
		t.Fatalf("testutil: start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("testutil: connection string: %v", err)
	}

	pool, err := database.Connect(dsn)
	if err != nil {
		t.Fatalf("testutil: connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := migrate.Run(ctx, pool); err != nil {
		t.Fatalf("testutil: migrate: %v", err)
	}

	return pool
}
