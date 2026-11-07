package guard_test

import (
	"testing"

	"github.com/nahuelsantos/contact-api/internal/guard"
)

func TestHoneypot(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]string
		want bool
	}{
		{"absent", map[string]string{"name": "a"}, false},
		{"empty", map[string]string{"_gotcha": ""}, false},
		{"whitespace", map[string]string{"_gotcha": " \t\n"}, false},
		{"filled", map[string]string{"_gotcha": "bot"}, true},
		{"nil map", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := guard.Honeypot(tt.in); got != tt.want {
				t.Fatalf("Honeypot = %v, want %v", got, tt.want)
			}
		})
	}
}
