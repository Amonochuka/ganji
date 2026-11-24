package validate

import (
	"strings"
	"testing"
)

func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  bool
	}{
		{name: "simple", email: "user@example.com", want: true},
		{name: "plus alias", email: "user+tag@example.co.uk", want: true},
		{name: "consecutive dots", email: "user..name@example.com", want: false},
		{name: "leading dot", email: ".user@example.com", want: false},
		{name: "missing tld", email: "user@localhost", want: false},
		{name: "double @", email: "user@@example.com", want: false},
		{name: "label too long", email: "user@" + strings.Repeat("a", 64) + ".com", want: false},
		{name: "too long", email: string(make([]byte, 255)), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidEmail(tt.email); got != tt.want {
				t.Fatalf("IsValidEmail(%q) = %v, want %v", tt.email, got, tt.want)
			}
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  User@Example.COM "); got != "user@example.com" {
		t.Fatalf("NormalizeEmail() = %q, want %q", got, "user@example.com")
	}
}
