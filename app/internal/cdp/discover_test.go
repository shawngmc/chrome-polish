package cdp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverBrowser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"Browser": "Chrome/152.0.0.0",
			"Protocol-Version": "1.3",
			"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/browser/abc-123"
		}`))
	}))
	defer srv.Close()

	info, err := DiscoverBrowser(context.Background(), strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("DiscoverBrowser() unexpected error: %v", err)
	}
	if info.Browser != "Chrome/152.0.0.0" {
		t.Errorf("info.Browser = %q, want %q", info.Browser, "Chrome/152.0.0.0")
	}
	if info.WebSocketDebuggerURL != "ws://127.0.0.1:9222/devtools/browser/abc-123" {
		t.Errorf("info.WebSocketDebuggerURL = %q, want the fixture URL", info.WebSocketDebuggerURL)
	}
}

func TestDiscoverBrowserMissingWebSocketURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Browser": "Chrome/152.0.0.0"}`))
	}))
	defer srv.Close()

	if _, err := DiscoverBrowser(context.Background(), strings.TrimPrefix(srv.URL, "http://")); err == nil {
		t.Error("DiscoverBrowser() with no webSocketDebuggerUrl in the response should error")
	}
}

func TestDiscoverBrowserNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if _, err := DiscoverBrowser(context.Background(), strings.TrimPrefix(srv.URL, "http://")); err == nil {
		t.Error("DiscoverBrowser() against a 404 (the flag-less toggle's /json/version, per cdp-remote-debugging-quirks) should error")
	}
}

func TestDiscoverBrowserMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	if _, err := DiscoverBrowser(context.Background(), strings.TrimPrefix(srv.URL, "http://")); err == nil {
		t.Error("DiscoverBrowser() with a malformed response body should error")
	}
}

func TestDiscoverBrowserUnreachable(t *testing.T) {
	if _, err := DiscoverBrowser(context.Background(), "127.0.0.1:1"); err == nil {
		t.Error("DiscoverBrowser() against an unreachable address should error")
	}
}
