package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrBadInput  = errors.New("bad input")
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func nextBusinessID(ctx context.Context, pool *pgxpool.Pool, table, prefix string) (string, error) {
	yy := time.Now().UTC().Format("06")
	pattern := fmt.Sprintf("%s-%s-%%", prefix, yy)
	q := fmt.Sprintf(`
		SELECT COALESCE(MAX(
			CAST(NULLIF(regexp_replace(business_id, '^%s-%s-', ''), '') AS INT)
		), 0) + 1
		FROM %s
		WHERE business_id LIKE $1`, prefix, yy, table)
	var n int
	if err := pool.QueryRow(ctx, q, pattern).Scan(&n); err != nil {
		return "", fmt.Errorf("next id: %w", err)
	}
	return fmt.Sprintf("%s-%s-%04d", prefix, yy, n), nil
}
