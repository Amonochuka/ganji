package deals

import (
	"context"
	"database/sql"
)

type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Repository struct {
	db          *sql.DB
	q           DBTX
	preimageKey []byte
}

func NewRepository(db *sql.DB, preimageSecret string) *Repository {
	return &Repository{
		db:          db,
		q:           db,
		preimageKey: preimageKey(preimageSecret),
	}
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r *Repository) WithTx(tx *sql.Tx) DealRepository {
	return &Repository{
		db:          r.db,
		q:           tx,
		preimageKey: r.preimageKey,
	}
}

func (r *Repository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, nil)
}
