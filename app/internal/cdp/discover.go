package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// BrowserInfo is the response from the browser's /json/version endpoint.
type BrowserInfo struct {
	Browser              string `json:"Browser"`
	ProtocolVersion      string `json:"Protocol-Version"`
	UserAgent            string `json:"User-Agent"`
	V8Version            string `json:"V8-Version"`
	WebKitVersion        string `json:"WebKit-Version"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// DiscoverBrowser fetches the browser-level CDP endpoint info from a
// Chrome/Chromium instance with remote debugging enabled. addr is a
// host:port such as "127.0.0.1:9222" or a relay's "host:port".
func DiscoverBrowser(ctx context.Context, addr string) (*BrowserInfo, error) {
	url := fmt.Sprintf("http://%s/json/version", addr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cdp: build request for %s: %w", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cdp: fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cdp: fetch %s: unexpected status %s", url, resp.Status)
	}

	var info BrowserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("cdp: decode response from %s: %w", url, err)
	}
	if info.WebSocketDebuggerURL == "" {
		return nil, fmt.Errorf("cdp: %s returned no webSocketDebuggerUrl", url)
	}

	return &info, nil
}
