package deals

import (
	"crypto/rand"
	"encoding/base64"
	"net/mail"
	"strings"
)

func generateShareToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// isValidEmail does a light syntactic check using the standard library. The
// authoritative validation happens when the client actually registers with
// this email (auth.Service lowercases + enforces a stricter pattern).
func isValidEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s && strings.Contains(s, "@")
}
