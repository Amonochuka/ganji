package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Repository struct {
	db *sql.DB
	q  DBTX
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
		q:  db,
	}
}

func (r *Repository) Create(ctx context.Context, email, passwordHash, displayName, slug string) (*User, error) {
	query := `
		INSERT INTO users (email, password_hash, display_name, slug)
		VALUES ($1, $2, $3, $4)
		RETURNING id, email, display_name, slug, bitcoin_address, trust_score, created_at
	`

	var u User
	var bitcoinAddress sql.NullString

	err := r.db.QueryRowContext(ctx, query, email, passwordHash, displayName, slug).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.Slug, &bitcoinAddress, &u.TrustScore, &u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: create user: %w", err)
	}

	u.BitcoinAddress = bitcoinAddress.String
	return &u, nil
}

func (r *Repository) FindByEmail(ctx context.Context, email string) (*User, error) {
	query := `
		SELECT id, email, password_hash, display_name, slug, bitcoin_address, trust_score, created_at
		FROM users
		WHERE email = $1
	`

	var u User
	var bitcoinAddress sql.NullString

	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Slug, &bitcoinAddress, &u.TrustScore, &u.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("repository: find user by email: %w", err)
	}

	u.BitcoinAddress = bitcoinAddress.String
	return &u, nil
}

// FindBySlug looks up a user by their public CV slug. Used for the public
// Live CV page — never includes password_hash by design.
func (r *Repository) FindBySlug(ctx context.Context, slug string) (*User, error) {
	query := `
		SELECT id, email, display_name, slug, bitcoin_address, trust_score, created_at
		FROM users
		WHERE slug = $1
	`

	var u User
	var bitcoinAddress sql.NullString

	err := r.db.QueryRowContext(ctx, query, slug).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.Slug, &bitcoinAddress, &u.TrustScore, &u.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("repository: find user by slug: %w", err)
	}

	u.BitcoinAddress = bitcoinAddress.String
	return &u, nil
}

func (r *Repository) EmailExists(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`, email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("repository: email exists: %w", err)
	}
	return exists, nil
}

// SlugExists checks if a slug is already taken.
func (r *Repository) SlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE slug = $1)`, slug).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("repository: slug exists: %w", err)
	}
	return exists, nil
}

func (r *Repository) StoreRefreshToken(ctx context.Context, token *StoredRefreshToken) error {
	query := `INSERT INTO refresh_tokens(user_id, token_hash, expires_at)
			VALUES($1, $2, $3) RETURNING id, created_at;`
	row := r.db.QueryRowContext(ctx, query, token.UserID, token.TokenHash, token.ExpiresAt)
	if err := row.Scan(&token.ID, &token.CreatedAt); err != nil {
		return fmt.Errorf("repository: store refresh token: %w", err)
	}
	return nil
}

func (r *Repository) FindRefreshToken(ctx context.Context, tokenHash string) (*StoredRefreshToken, error) {
	query := `
		SELECT id, user_id,token_hash,expires_at, created_at FROM refresh_tokens
		WHERE token_hash = $1;
	`
	token := StoredRefreshToken{}
	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&token.ID,
		&token.UserID,
		&token.TokenHash,
		&token.ExpiresAt,
		&token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRefreshTokenNotFound
		}
		return nil, fmt.Errorf("repository: find refresh token: %w", err)
	}

	return &token, nil
}

func (r *Repository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	query := `
		DELETE FROM refresh_tokens WHERE token_hash = $1;`

	result, err := r.db.ExecContext(ctx, query, tokenHash)
	if err != nil {
		return fmt.Errorf("repository: revoke refresh token: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("repository: revoke refresh token: %w", err)
	}
	if rows == 0 {
		return ErrRefreshTokenNotFound
	}
	return nil
}
