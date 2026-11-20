package deals

import (
	"fmt"
	"net/url"
	"strings"
)

func isHoldSettled(status string) bool {
	switch strings.ToUpper(status) {
	case "SETTLED", "SUCCESS", "COMPLETE", "SUCCEEDED", "PAID":
		return true
	default:
		return false
	}
}

// isPayoutConfirmed reports whether an outgoing payment status means the
// freelancer got paid. LNbits backends report the underlying backend status
// verbatim, so we accept the common set instead of hard-coding one backend.
func isPayoutConfirmed(status string) bool {
	return isHoldSettled(status)
}

// isPayoutFailed reports whether an outgoing payment status definitively means
// nothing was sent.
func isPayoutFailed(status string) bool {
	switch strings.ToUpper(status) {
	case "FAILED", "UNPAID", "CANCELLED", "EXPIRED":
		return true
	default:
		return false
	}
}

// isHoldReleased reports whether an LNbits payment status means the funds are
// no longer committed to the escrow (already returned to the payer).
func isHoldReleased(status string) bool {
	switch status {
	case "UNPAID", "EXPIRED", "CANCELLED":
		return true
	default:
		return false
	}
}

func validateWalletEndpoint(raw string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: invalid wallet URL", ErrInvalidInput)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
