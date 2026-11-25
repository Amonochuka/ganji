package deals

import (
	"fmt"
	"net"
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

// privateOrLoopbackCIDRs is the set of network ranges that must never be
// reachable via a caller-supplied wallet URL. Allowing these would let an
// attacker with a valid share token use the backend as an SSRF proxy to
// probe internal infrastructure (databases, cloud metadata endpoints, etc.).
var privateOrLoopbackCIDRs = func() []*net.IPNet {
	blocks := []string{
		"0.0.0.0/8",      // Unspecified / routes to localhost on many systems
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"169.254.0.0/16", // Link-local / AWS & GCP metadata
		"100.64.0.0/10",  // Shared address space (RFC 6598)
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 ULA
		"fe80::/10",      // IPv6 link-local
	}
	out := make([]*net.IPNet, 0, len(blocks))
	for _, b := range blocks {
		_, cidr, err := net.ParseCIDR(b)
		if err != nil {
			panic(err)
		}
		out = append(out, cidr)
	}
	return out
}()

// isPrivateOrLoopback returns true when ip falls within any range that should
// not be reachable from a caller-supplied URL.
func isPrivateOrLoopback(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}
	for _, cidr := range privateOrLoopbackCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// validateWalletEndpoint parses and sanitises a caller-supplied LNbits wallet
// URL. It rejects:
//   - non-http(s) schemes
//   - embedded credentials (user info)
//   - query strings and fragments (not used by LNbits API calls)
//   - any hostname that resolves to a loopback, private, or link-local address
//     (SSRF guard — prevents using the backend as a proxy into internal infra)
//
// The resolved IP check uses the hostname as supplied; it is not a substitute
// for a full SSRF-safe HTTP client, but it eliminates the most obvious attack
// vectors for this endpoint.
var walletEndpointValidator = validateWalletEndpointStrict

func validateWalletEndpoint(raw string) (string, error) {
	return walletEndpointValidator(raw)
}

func validateWalletEndpointStrict(raw string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: invalid wallet URL", ErrInvalidInput)
	}

	hostname := parsed.Hostname()

	// Reject bare IP addresses in blocked ranges immediately — no DNS needed.
	if ip := net.ParseIP(hostname); ip != nil {
		if isPrivateOrLoopback(ip) {
			return "", fmt.Errorf("%w: wallet URL must not point to a private or loopback address", ErrInvalidInput)
		}
	} else {
		// Hostname: resolve and check every returned address.
		addrs, err := net.LookupHost(hostname)
		if err != nil {
			return "", fmt.Errorf("%w: cannot resolve wallet URL host: %v", ErrInvalidInput, err)
		}
		for _, addr := range addrs {
			ip := net.ParseIP(addr)
			if ip == nil {
				continue
			}
			if isPrivateOrLoopback(ip) {
				return "", fmt.Errorf("%w: wallet URL resolves to a private or loopback address", ErrInvalidInput)
			}
		}
	}

	return strings.TrimRight(parsed.String(), "/"), nil
}
