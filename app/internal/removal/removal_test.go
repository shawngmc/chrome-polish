package removal

import (
	"reflect"
	"testing"
)

func TestSplitCookies(t *testing.T) {
	tests := []struct {
		name        string
		types       []StorageType
		wantCookies bool
		wantRest    []StorageType
	}{
		{
			name:        "cookies among others",
			types:       []StorageType{Cookies, LocalStorage, ServiceWorkers},
			wantCookies: true,
			wantRest:    []StorageType{LocalStorage, ServiceWorkers},
		},
		{
			name:        "no cookies",
			types:       []StorageType{LocalStorage, CacheStorage},
			wantCookies: false,
			wantRest:    []StorageType{LocalStorage, CacheStorage},
		},
		{
			name:        "only cookies",
			types:       []StorageType{Cookies},
			wantCookies: true,
			wantRest:    nil,
		},
		{
			name:        "empty",
			types:       nil,
			wantCookies: false,
			wantRest:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCookies, gotRest := splitCookies(tt.types)
			if gotCookies != tt.wantCookies {
				t.Errorf("splitCookies(%v) cookies = %v, want %v", tt.types, gotCookies, tt.wantCookies)
			}
			if !reflect.DeepEqual(gotRest, tt.wantRest) {
				t.Errorf("splitCookies(%v) rest = %v, want %v", tt.types, gotRest, tt.wantRest)
			}
		})
	}
}

func TestCookieMatchesHost(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		host   string
		want   bool
	}{
		{"host-only exact match", "example.com", "example.com", true},
		{"host-only, different host", "example.com", "other.com", false},
		{"domain cookie matches its own host", ".example.com", "example.com", true},
		{"domain cookie matches a subdomain", ".example.com", "sub.example.com", true},
		{"domain cookie does not match unrelated host", ".example.com", "notexample.com", false},
		{"domain cookie does not match a look-alike suffix", ".example.com", "evilexample.com", false},
		{"host-only cookie does not match a subdomain", "example.com", "sub.example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cookieMatchesHost(tt.domain, tt.host); got != tt.want {
				t.Errorf("cookieMatchesHost(%q, %q) = %v, want %v", tt.domain, tt.host, got, tt.want)
			}
		})
	}
}
