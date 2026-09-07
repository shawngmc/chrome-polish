package scan

import "testing"

func TestNormalizeOrigin(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"https default port dropped", "https://example.com:443", "https://example.com"},
		{"http default port dropped", "http://example.com:80", "http://example.com"},
		{"non-default port kept", "https://example.com:8443", "https://example.com:8443"},
		{"no port", "https://example.com", "https://example.com"},
		{"http non-default port kept", "http://example.com:8080", "http://example.com:8080"},
		{"missing scheme", "example.com", ""},
		{"empty string", "", ""},
		{"garbage", "not a url at all", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeOrigin(tt.raw); got != tt.want {
				t.Errorf("normalizeOrigin(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestEntryOrigin(t *testing.T) {
	entry := &DOMNode{
		Tag: "site-list-entry",
		Children: []DOMNode{
			{Tag: "span", Attrs: map[string]string{"class": "favicon"}},
			{Tag: "span", Attrs: map[string]string{"class": "url-directionality"}, Children: []DOMNode{
				{Text: "https://bad.example:443"},
			}},
		},
	}

	if got, want := entryOrigin(entry), "https://bad.example"; got != want {
		t.Errorf("entryOrigin() = %q, want %q", got, want)
	}

	noMatch := &DOMNode{Tag: "site-list-entry", Children: []DOMNode{
		{Tag: "span", Attrs: map[string]string{"class": "favicon"}},
	}}
	if got := entryOrigin(noMatch); got != "" {
		t.Errorf("entryOrigin() with no url-directionality span = %q, want empty", got)
	}
}

func TestSectionStatus(t *testing.T) {
	headerStatus := CategoryNotifications.HeaderStatus

	allowed := &DOMNode{Tag: "site-list", Children: []DOMNode{
		{Tag: "h3", Children: []DOMNode{{Text: "Allowed to send notifications"}}},
	}}
	status, ok := sectionStatus(allowed, headerStatus)
	if !ok || status != PermissionAllow {
		t.Errorf("sectionStatus(allowed) = (%v, %v), want (%v, true)", status, ok, PermissionAllow)
	}

	blocked := &DOMNode{Tag: "site-list", Children: []DOMNode{
		{Tag: "h3", Children: []DOMNode{{Text: "  Not allowed to send notifications  "}}},
	}}
	status, ok = sectionStatus(blocked, headerStatus)
	if !ok || status != PermissionBlock {
		t.Errorf("sectionStatus(blocked, with whitespace) = (%v, %v), want (%v, true)", status, ok, PermissionBlock)
	}

	unrecognized := &DOMNode{Tag: "site-list", Children: []DOMNode{
		{Tag: "h3", Children: []DOMNode{{Text: "Some future Chrome wording"}}},
	}}
	if _, ok := sectionStatus(unrecognized, headerStatus); ok {
		t.Error("sectionStatus should skip an unrecognized header rather than guess, per DESIGN.md's degrade-gracefully rule")
	}

	noHeader := &DOMNode{Tag: "site-list"}
	if _, ok := sectionStatus(noHeader, headerStatus); ok {
		t.Error("sectionStatus with no h3 header should report not-ok")
	}
}
