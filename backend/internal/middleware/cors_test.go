package middleware

import "testing"

func TestIsPrivateLANDevOrigin(t *testing.T) {
	tests := []struct {
		origin string
		want   bool
	}{
		{"http://192.168.89.197:3000", true},
		{"http://192.168.1.149:3000", true},
		{"http://10.0.0.8:3000", true},
		{"http://172.16.0.1:3000", true},
		{"http://172.31.255.254:3000", true},
		{"http://LAP-044.local:3000", true},
		{"http://8.8.8.8:3000", false},
		{"http://localhost:3000", false},
		{"http://192.168.1.149:8080", false},
		{"https://192.168.1.149:3000", false},
		{"http://192.168.1.149:3000/wallet", false},
	}

	for _, test := range tests {
		if got := isPrivateLANDevOrigin(test.origin); got != test.want {
			t.Errorf("isPrivateLANDevOrigin(%q) = %t, want %t", test.origin, got, test.want)
		}
	}
}
