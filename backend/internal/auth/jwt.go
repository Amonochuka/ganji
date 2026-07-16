package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("invalid or expired token")

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

// Claims is the JWT payload. Subject is the user ID — never put the
// password hash or anything sensitive in here, JWTs are signed, not
// encrypted. Anyone can decode and read the payload.
type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// TokenManager issues and verifies JWTs using two separate secrets — one
// for access tokens, one for refresh tokens. A leaked access secret then
// doesn't also compromise refresh tokens.
type TokenManager struct {
	accessSecret  []byte
	refreshSecret []byte
}

func NewTokenManager(accessSecret, refreshSecret string) *TokenManager {
	return &TokenManager{
		accessSecret:  []byte(accessSecret),
		refreshSecret: []byte(refreshSecret),
	}
}

// GenerateAccessToken creates a short-lived token used to authenticate
// API requests. Short TTL limits damage if a token leaks.
func (tm *TokenManager) GenerateAccessToken(userID, email string) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(accessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tm.accessSecret)
}

// GenerateRefreshToken creates a long-lived token used only to obtain new
// access tokens.
func (tm *TokenManager) GenerateRefreshToken(userID, email string) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(refreshTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tm.refreshSecret)
}

// VerifyAccessToken parses and validates an access token, returning its
// claims if valid.
func (tm *TokenManager) VerifyAccessToken(tokenString string) (*Claims, error) {
	return tm.verify(tokenString, tm.accessSecret)
}

// VerifyRefreshToken parses and validates a refresh token.
func (tm *TokenManager) VerifyRefreshToken(tokenString string) (*Claims, error) {
	return tm.verify(tokenString, tm.refreshSecret)
}

func (tm *TokenManager) verify(tokenString string, secret []byte) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return secret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

func (tm *TokenManager) RefreshTokenTTL() time.Duration {
	return refreshTokenTTL
}