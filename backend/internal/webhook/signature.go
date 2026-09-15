package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// signatureTolerance is how far a webhook timestamp may be from now before
// we reject it. Mirrors LNbits's own 5-minute window (LNbits PR #4016).
const signatureTolerance = 5 * time.Minute

// VerifyLNbitsSignature checks the LNbits-Signature header the way LNbits
// protects outgoing webhooks: the wallet's webhook_secret is the HMAC-SHA256
// key, and the signed payload is "{timestamp}.{raw_body}". Replays are
// rejected by requiring the timestamp to be close to now.
func VerifyLNbitsSignature(rawBody []byte, signatureHeader, secret string) bool {
	if secret == "" {
		return true
	}
	if signatureHeader == "" {
		return false
	}

	items := make(map[string]string)
	for _, part := range strings.Split(signatureHeader, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return false
		}
		items[strings.TrimSpace(kv[0])] = kv[1]
	}

	timestamp, err := strconv.ParseInt(items["t"], 10, 64)
	if err != nil {
		return false
	}

	if diff := time.Since(time.Unix(timestamp, 0)); diff < -signatureTolerance || diff > signatureTolerance {
		return false
	}

	expected := hmac.New(sha256.New, []byte(secret))
	expected.Write([]byte(items["t"] + "." + string(rawBody)))

	got, err := hex.DecodeString(items["v1"])
	if err != nil {
		return false
	}

	return hmac.Equal(expected.Sum(nil), got)
}
