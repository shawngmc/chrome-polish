package cdp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDevToolsActivePort(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{
			name:    "well-formed file",
			content: "12345\n/devtools/browser/abc-123\n",
			want:    "ws://127.0.0.1:12345/devtools/browser/abc-123",
		},
		{
			name:    "no trailing newline",
			content: "12345\n/devtools/browser/abc-123",
			want:    "ws://127.0.0.1:12345/devtools/browser/abc-123",
		},
		{
			name:    "extra whitespace trimmed",
			content: "  12345  \n  /devtools/browser/abc-123  \n",
			want:    "ws://127.0.0.1:12345/devtools/browser/abc-123",
		},
		{
			name:    "only one line",
			content: "12345\n",
			wantErr: true,
		},
		{
			name:    "empty port",
			content: "\n/devtools/browser/abc-123\n",
			wantErr: true,
		},
		{
			name:    "empty path",
			content: "12345\n\n",
			wantErr: true,
		},
		{
			name:    "empty file",
			content: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			got, err := readDevToolsActivePort(dir)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("readDevToolsActivePort() = %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("readDevToolsActivePort() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("readDevToolsActivePort() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadDevToolsActivePortMissingFile(t *testing.T) {
	dir := t.TempDir() // no DevToolsActivePort written
	if _, err := readDevToolsActivePort(dir); err == nil {
		t.Error("readDevToolsActivePort() with no such file should error")
	}
}

func TestUserDataDirUnsupportedBrowser(t *testing.T) {
	if _, err := UserDataDir(Browser("not-a-real-browser")); err == nil {
		t.Error("UserDataDir() for an unknown browser should error")
	}
}
