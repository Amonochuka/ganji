package deals

import (
	"crypto/rand"
	"encoding/base64"

	"github.com/Amonochuka/ganji-backend/internal/validate"
)

func generateShareToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func isValidEmail(s string) bool {
	return validate.IsValidEmail(validate.NormalizeEmail(s))
}
