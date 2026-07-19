package auth

import "errors"

var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrInvalidEmail         = errors.New("invalid email format")
	ErrPasswordTooShort     = errors.New("password must be at least 8 characters")
	ErrEmailTaken           = errors.New("email is already registered")
	ErrSlugTaken            = errors.New("slug is already taken")
	ErrInvalidCredentials   = errors.New("invalid email or password")
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrInvalidToken         = errors.New("invalid or expired token")
)
