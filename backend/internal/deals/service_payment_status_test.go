package deals

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateWalletEndpointWithDevAllowlist(t *testing.T) {
	t.Parallel()

	svc := NewService(nil, nil, WithDevAllowedWalletURLs([]string{
		"http://localhost:5001/",
		"http://localhost:5002",
	}))

	for _, tc := range []struct {
		name string
		url  string
	}{
		{"client wallet", "http://localhost:5001"},
		{"freelancer wallet", "http://localhost:5002"},
		{"trailing slash", "http://localhost:5002/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			endpoint, err := svc.validateWalletEndpoint(tc.url)
			if err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if endpoint != strings.TrimRight(tc.url, "/") {
				t.Fatalf("expected canonical endpoint %q, got %q", strings.TrimRight(tc.url, "/"), endpoint)
			}
		})
	}

	for _, tc := range []struct {
		name string
		url  string
	}{
		{"escrow wallet not allowlisted", "http://localhost:5000"},
		{"ip alias not allowlisted", "http://127.0.0.1:5002"},
		{"scheme mismatch", "https://localhost:5002"},
		{"path rejected", "http://localhost:5002/wallet"},
		{"metadata endpoint", "http://169.254.169.254"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := svc.validateWalletEndpoint(tc.url); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput for %q, got %v", tc.url, err)
			}
		})
	}
}

func TestValidateWalletEndpoint(t *testing.T) {
	t.Parallel()

	okCases := []struct {
		name string
		url  string
	}{
		// Use routable public IPs directly to avoid DNS in test environments.
		// 1.1.1.1 is Cloudflare's public DNS resolver — clearly not a private range.
		{"https public IP", "https://1.1.1.1"},
		{"http public IP with path", "http://1.1.1.1/wallet"},
		{"https public IP with port", "https://1.1.1.1:443"},
	}
	for _, tc := range okCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := validateWalletEndpoint(tc.url); err != nil {
				t.Errorf("expected ok, got %v", err)
			}
		})
	}

	ssrfCases := []struct {
		name string
		url  string
	}{
		{"loopback ip", "http://127.0.0.1"},
		{"loopback ip 2", "http://127.0.0.2:5000"},
		{"unspecified ip", "http://0.0.0.0"},
		{"unspecified ip with port", "http://0.0.0.0:5000"},
		{"ipv4-mapped loopback", "http://[::ffff:127.0.0.1]"},
		{"localhost hostname", "http://localhost"},
		{"private 10.x", "http://10.0.0.1"},
		{"private 192.168.x", "http://192.168.1.1"},
		{"private 172.16.x", "http://172.16.0.1"},
		{"link-local metadata", "http://169.254.169.254"},
		{"ipv6 loopback", "http://[::1]"},
	}
	for _, tc := range ssrfCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateWalletEndpoint(tc.url)
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput for SSRF URL %q, got %v", tc.url, err)
			}
		})
	}

	malformedCases := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"no scheme", "lnbits.example.com"},
		{"ftp scheme", "ftp://lnbits.example.com"},
		{"with credentials", "http://user:pass@lnbits.example.com"},
		{"with query", "http://lnbits.example.com?foo=bar"},
		{"with fragment", "http://lnbits.example.com#frag"},
	}
	for _, tc := range malformedCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateWalletEndpoint(tc.url)
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput for malformed URL %q, got %v", tc.url, err)
			}
		})
	}
}
