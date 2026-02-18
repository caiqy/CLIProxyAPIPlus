package management

import "testing"

func TestSanitizeOAuthCallbackHost(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty uses default", raw: "", want: "localhost"},
		{name: "plain host", raw: "dev.local", want: "dev.local"},
		{name: "url with scheme path and port", raw: "http://dev.local:1455/auth/callback", want: "dev.local"},
		{name: "invalid host fallback", raw: "dev/local", want: "localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeOAuthCallbackHost(tt.raw)
			if got != tt.want {
				t.Fatalf("sanitizeOAuthCallbackHost(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
