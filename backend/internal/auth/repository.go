package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Repository wraps raw SQL access to the users table. No business logic
// lives here — just queries. Validation and password hashing belong in
// Service, which calls this repository.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Create inserts a new user and returns the generated ID and created_at.
func (r *Repository) Create(email, passwordHash, displayName, slug string) (*User, error) {
	query := `
		INSERT INTO users (email, password_hash, display_name, slug)
		VALUES ($1, $2, $3, $4)
		RETURNING id, email, display_name, slug, bitcoin_address, trust_score, created_at
	`

	var u User
	var bitcoinAddress sql.NullString

	err := r.db.QueryRow(query, email, passwordHash, displayName, slug).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.Slug, &bitcoinAddress, &u.TrustScore, &u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	u.BitcoinAddress = bitcoinAddress.String
	return &u, nil
}

// FindByEmail looks up a user by email, including password_hash — needed
// for login to verify the password. Returns nil, nil if no user is found.
func (r *Repository) FindByEmail(email string) (*User, error) {
	query := `
		SELECT id, email, password_hash, display_name, slug, bitcoin_address, trust_score, created_at
		FROM users
		WHERE email = $1
	`

	var u User
	var bitcoinAddress sql.NullString

	err := r.db.QueryRow(query, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Slug, &bitcoinAddress, &u.TrustScore, &u.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find user by email: %w", err)
	}

	u.BitcoinAddress = bitcoinAddress.String
	return &u, nil
}

// FindBySlug looks up a user by their public CV slug. Used for the public
// Live CV page — never includes password_hash by design.
func (r *Repository) FindBySlug(slug string) (*User, error) {
	query := `
		SELECT id, email, display_name, slug, bitcoin_address, trust_score, created_at
		FROM users
		WHERE slug = $1
	`

	var u User
	var bitcoinAddress sql.NullString

	err := r.db.QueryRow(query, slug).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.Slug, &bitcoinAddress, &u.TrustScore, &u.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find user by slug: %w", err)
	}

	u.BitcoinAddress = bitcoinAddress.String
	return &u, nil
}

// EmailExists checks if an email is already registered.
func (r *Repository) EmailExists(email string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`, email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check email existence: %w", err)
	}
	return exists, nil
}

// SlugExists checks if a slug is already taken.
func (r *Repository) SlugExists(slug string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE slug = $1)`, slug).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check slug existence: %w", err)
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
