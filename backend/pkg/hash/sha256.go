package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
)

// SumSHA256 returns the lowercase hex-encoded SHA-256 digest of data. Used
// as the content-verification anchor for Live CV entries: the hash binds a
// CV line to the artifact's storage reference at release time, and the
// verify endpoint recomputes it to prove the entry is intact.
func SumSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// NewSHA256 returns a new hash.Hash computing the SHA256 checksum.
func NewSHA256() hash.Hash {
	return sha256.New()
}
