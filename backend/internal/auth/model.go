package auth

import "time"

// User mirrors the users table. password_hash is deliberately excluded
// from any JSON response — see the json:"-" tag. Never let a password
// hash leave the backend, even by accident.
type User struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	PasswordHash   string    `json:"-"`
	DisplayName    string    `json:"display_name"`
	Slug           string    `json:"slug"`
	BitcoinAddress string    `json:"bitcoin_address"`
	TrustScore     float64   `json:"trust_score"`
	CreatedAt      time.Time `json:"created_at"`
}

type StoredRefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type AuthResponse struct {
	User         *User  `json:"user"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}
