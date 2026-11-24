package auth

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "ascii", input: "Alice Smith", want: "alice-smith"},
		{name: "accents", input: " José Álvarez ", want: "jose-alvarez"},
		{name: "punctuation", input: "a_b--c", want: "a-b-c"},
		{name: "non latin", input: "张伟", want: "user-a3a0bafd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slugify(tt.input); got != tt.want {
				t.Fatalf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSlugifyFallbackIsDeterministic(t *testing.T) {
	first := slugify("张伟")
	second := slugify("张伟")
	if first != second {
		t.Fatalf("slugify fallback not deterministic: %q != %q", first, second)
	}
}
