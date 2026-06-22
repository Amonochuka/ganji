package auth

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrEmailTaken         = errors.New("email is already registered")
	ErrSlugTaken          = errors.New("slug is already taken")
	ErrInvalidEmail       = errors.New("invalid email format")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

var emailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// Service holds the business logic for registering and authenticating
// users. It owns validation and hashing — Repository is pure SQL and
// knows nothing about passwords being plaintext or hashed.
type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// Register validates input, checks for conflicts, hashes the password,
// generates a slug from the display name, and creates the user.
func (s *Service) Register(email, password, displayName string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	if !emailPattern.MatchString(email) {
		return nil, ErrInvalidEmail
	}
	if len(password) < 8 {
		return nil, ErrPasswordTooShort
	}
	if strings.TrimSpace(displayName) == "" {
		return nil, errors.New("display name is required")
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

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	user, err := s.repo.Create(email, string(hash), strings.TrimSpace(displayName), slug)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	return user, nil
}

// Authenticate verifies an email/password pair against the stored hash.
// Returns ErrInvalidCredentials for both "no such user" and "wrong
// password" — never reveal which one it was.
func (s *Service) Authenticate(email, password string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))

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

	return "", errors.New("could not generate a unique slug after 100 attempts")
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