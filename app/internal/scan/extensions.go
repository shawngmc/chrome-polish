package scan

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

const extensionsInternalsURL = "chrome://extensions-internals"

// extensionsInternalsScript reads the plain <pre> text chrome://extensions-
// internals renders — a top-level JSON array of every installed
// extension/app's internal state, confirmed via dom-probe against a live
// profile. Unlike chrome://extensions, this isn't a Polymer/Lit WebUI with
// data buried in private component properties: it's a stable debug dump
// meant to be read exactly this way, so no shadow-DOM piercing or custom
// element lookup is needed. It still polls for the <pre> rather than
// reading it immediately after navigation, though: the page takes a
// moment to populate (confirmed live — an immediate read returns before
// the element exists).
const extensionsInternalsScript = `
(async function(timeoutMs) {
  var start = Date.now();
  while (true) {
    var el = document.querySelector('pre');
    if (el && el.textContent) return el.textContent;
    if (Date.now() - start > timeoutMs) return '';
    await new Promise(function(resolve) { setTimeout(resolve, 50); });
  }
})(%d)
`

type extensionInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DiscoverExtensionNames reads every installed extension/app's display
// name from chrome://extensions-internals — a local, static Chrome page,
// never a candidate/suspect origin — keyed by the extension ID that also
// appears as the host in that extension's chrome-extension:// origin(s).
// Runs in the background (no foreground tab switch): unlike
// chrome://settings' all-sites list (see cdp-remote-debugging-quirks item
// 4), this page's content isn't behind iron-list virtualization needing
// real layout.
func DiscoverExtensionNames(ctx context.Context, client *cdp.Client) (map[string]string, error) {
	sessionID, cleanup, err := cdp.AttachTarget(ctx, client, extensionsInternalsURL, true)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	defer cleanup()

	var evalResult struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	script := fmt.Sprintf(extensionsInternalsScript, 5000)
	if err := client.CallSession(ctx, sessionID, "Runtime.evaluate", map[string]any{
		"expression":    script,
		"returnByValue": true,
		"awaitPromise":  true,
	}, &evalResult); err != nil {
		return nil, fmt.Errorf("scan: Runtime.evaluate on %s: %w", extensionsInternalsURL, err)
	}
	if evalResult.ExceptionDetails != nil {
		return nil, fmt.Errorf("scan: script threw on %s: %s", extensionsInternalsURL, evalResult.ExceptionDetails.Text)
	}
	if evalResult.Result.Value == "" {
		return nil, fmt.Errorf("scan: %s's <pre> not found", extensionsInternalsURL)
	}

	var infos []extensionInfo
	if err := json.Unmarshal([]byte(evalResult.Result.Value), &infos); err != nil {
		return nil, fmt.Errorf("scan: decode extensions-internals data: %w", err)
	}

	names := make(map[string]string, len(infos))
	for _, e := range infos {
		if e.ID != "" && e.Name != "" {
			names[e.ID] = e.Name
		}
	}
	return names, nil
}

// ExtensionID returns the extension ID from a chrome-extension:// origin,
// and whether origin actually is one.
func ExtensionID(origin string) (id string, ok bool) {
	const prefix = "chrome-extension://"
	if len(origin) <= len(prefix) || origin[:len(prefix)] != prefix {
		return "", false
	}
	return origin[len(prefix):], true
}
