package auth

import (
	"database/sql"
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
