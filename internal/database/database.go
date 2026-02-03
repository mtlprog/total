package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*DB, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{pool: pool}, nil
}

func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

func (db *DB) Close() {
	db.pool.Close()
}

func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	goose.SetBaseFS(migrations)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set dialect: %w", err)
	}

	db := stdlib.OpenDBFromPool(pool)
	defer func(db *sql.DB) {
		_ = db.Close()
	}(db)

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
