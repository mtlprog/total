package repository

import (
	"context"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mtlprog/total/internal/model"
)

type Repository struct {
	pool *pgxpool.Pool
	sq   squirrel.StatementBuilderType
}

func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, fmt.Errorf("pool is nil")
	}
	return &Repository{
		pool: pool,
		sq:   squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
	}, nil
}

func (r *Repository) GetExample(ctx context.Context, id int64) (*model.Example, error) {
	query, args, err := r.sq.
		Select("id", "name", "created_at", "updated_at").
		From("example").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	var e model.Example
	err = r.pool.QueryRow(ctx, query, args...).Scan(&e.ID, &e.Name, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to query example: %w", err)
	}

	return &e, nil
}

func (r *Repository) ListExamples(ctx context.Context) ([]*model.Example, error) {
	query, args, err := r.sq.
		Select("id", "name", "created_at", "updated_at").
		From("example").
		OrderBy("created_at DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query examples: %w", err)
	}
	defer rows.Close()

	var examples []*model.Example
	for rows.Next() {
		var e model.Example
		if err := rows.Scan(&e.ID, &e.Name, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan example: %w", err)
		}
		examples = append(examples, &e)
	}

	return examples, nil
}
