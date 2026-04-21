package pgstore

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate runs all pending Postgres migrations against the pool's underlying
// database. It opens a second database/sql handle from the pool's config —
// goose requires database/sql, pgx is used everywhere else.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	db, err := sql.Open("pgx", pool.Config().ConnString())
	if err != nil {
		return fmt.Errorf("migrate: open sql handle: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("migrate: set dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("migrate: up: %w", err)
	}
	return nil
}
