package scan

import "testing"

func TestCookieOrigin(t *testing.T) {
	tests := []struct {
		name string
		c    cdpCookie
		want string
	}{
		{"host-only cookie, secure", cdpCookie{Domain: "example.com", SourceScheme: "Secure"}, "https://example.com"},
		{"domain cookie, leading dot stripped", cdpCookie{Domain: ".example.com", SourceScheme: "Secure"}, "https://example.com"},
		{"non-secure cookie", cdpCookie{Domain: "example.com", SourceScheme: "NonSecure"}, "http://example.com"},
		{"unset scheme defaults to https", cdpCookie{Domain: "example.com", SourceScheme: "Unset"}, "https://example.com"},
		{"empty domain", cdpCookie{Domain: "", SourceScheme: "Secure"}, ""},
		{"domain is only a dot", cdpCookie{Domain: ".", SourceScheme: "Secure"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cookieOrigin(tt.c); got != tt.want {
				t.Errorf("cookieOrigin(%+v) = %q, want %q", tt.c, got, tt.want)
			}
		})
	}
}

func TestScopeOrigin(t *testing.T) {
	tests := []struct {
		name     string
		scopeURL string
		want     string
	}{
		{"simple scope", "https://example.com/", "https://example.com"},
		{"scope with path", "https://example.com/push/", "https://example.com"},
		{"scope with port", "https://example.com:8443/", "https://example.com:8443"},
		{"invalid url", "not a url", ""},
		{"no scheme", "example.com/push/", ""},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scopeOrigin(tt.scopeURL); got != tt.want {
				t.Errorf("scopeOrigin(%q) = %q, want %q", tt.scopeURL, got, tt.want)
			}
		})
	}
}
