package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var emailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// Service holds the business logic for registering and authenticating
// users. It owns validation and hashing — Repository is pure SQL and
// knows nothing about passwords being plaintext or hashed.
type Service struct {
	repo   *Repository
	tokens *TokenManager
}

func NewService(repo *Repository, tokens *TokenManager) *Service {
	return &Service{
		repo:   repo,
		tokens: tokens,
	}
}

// Register validates input, checks for conflicts, hashes the password,
// generates a slug from the display name, and creates the user.
func (s *Service) Register(ctx context.Context, email, password, displayName string) (*AuthResponse, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	if !emailPattern.MatchString(email) {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, ErrInvalidEmail)
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, ErrPasswordTooShort)
	}
	if strings.TrimSpace(displayName) == "" {
		return nil, fmt.Errorf("%w: display name is required", ErrInvalidInput)
	}

	taken, err := s.repo.EmailExists(email)
	if err != nil {
		return nil, fmt.Errorf("checking email availability: %w", err)
	}
	if taken {
		return nil, ErrEmailTaken
	}

	slug, err := s.generateUniqueSlug(displayName)
	if err != nil {
		return nil, fmt.Errorf("generating slug: %w", err)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	user, err := s.repo.Create(email, string(passwordHash), strings.TrimSpace(displayName), slug)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	accessToken, err := s.tokens.GenerateAccessToken(user.ID, user.Email)
	if err != nil {
		return nil, fmt.Errorf("generating access token: %w", err)
	}

	refreshToken, err := s.tokens.GenerateRefreshToken(user.ID, user.Email)
	if err != nil {
		return nil, fmt.Errorf("generating refresh token: %w", err)
	}

	hash := sha256.Sum256([]byte(refreshToken))
	tokenHash := hex.EncodeToString(hash[:])

	err = s.repo.StoreRefreshToken(ctx, &RefreshToken{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(s.tokens.RefreshTokenTTL()),
	})
	if err != nil {
		return nil, fmt.Errorf("storing refresh token: %w", err)
	}

	return &AuthResponse{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// Authenticate verifies an email/password pair against the stored hash.
// Returns ErrInvalidCredentials for both "no such user" and "wrong
// password" — never reveal which one it was.
func (s *Service) Authenticate(email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	if !emailPattern.MatchString(email) {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, ErrInvalidEmail)
	}
	if strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("%w: password is required", ErrInvalidInput)
	}

	user, err := s.repo.FindByEmail(email)
	if err != nil {
		return nil, fmt.Errorf("looking up user: %w", err)
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

// generateUniqueSlug turns a display name into a URL-safe slug and
// appends a numeric suffix if it's already taken (e.g. "juma-codes-2").
func (s *Service) generateUniqueSlug(displayName string) (string, error) {
	base := slugify(displayName)
	slug := base

	for i := 1; i < 100; i++ {
		taken, err := s.repo.SlugExists(slug)
		if err != nil {
			return "", err
		}
		if !taken {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", base, i+1)
	}

	return "", fmt.Errorf("%w: could not generate a unique slug after 100 attempts", ErrInvalidInput)
}

// slugify lowercases, replaces spaces with hyphens, and strips anything
// that isn't a letter, number, or hyphen.
func slugify(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastWasHyphen := false

	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasHyphen = false
		case r == ' ' || r == '-' || r == '_':
			if !lastWasHyphen && b.Len() > 0 {
				b.WriteRune('-')
				lastWasHyphen = true
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "user"
	}
	return slug
}
