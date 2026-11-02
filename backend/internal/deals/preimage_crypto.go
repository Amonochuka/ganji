package deals

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const preimageEncPrefix = "enc:"

// preimageKey derives a 32-byte AES key from the configured secret. An empty
// secret disables encryption (dev mode).
func preimageKey(secret string) []byte {
	if secret == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func encryptPreimage(key []byte, plaintext string) (string, error) {
	if plaintext == "" || len(key) == 0 {
		return plaintext, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return preimageEncPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

func decryptPreimage(key []byte, value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, preimageEncPrefix) {
		return value, nil
	}
	if len(key) == 0 {
		return "", errors.New("encrypted preimage present but no key configured")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, preimageEncPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("encrypted preimage too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
