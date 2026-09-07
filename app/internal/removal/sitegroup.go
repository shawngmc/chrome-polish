package removal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

// ClearSiteGroup wholly clears one eTLD+1 site grouping — every origin
// under it, partitioned or not, cookies and every other storage type at
// once — via chrome://settings/content/all's own removal primitive
// (browserProxy.clearSiteGroupDataAndCookies(groupingKey)), the same one
// its own "remove this site" action calls. groupingKey comes from
// scan.SiteGroup.GroupingKey (via scan.DiscoverSiteData / MergeSiteData's
// homeGroup lookup) — Chrome's own opaque, stable identifier for the
// group, not something to construct by hand.
//
// This exists because no CDP protocol command reaches storage-partitioned
// data at all: Storage.clearDataForOrigin only ever touches an origin's
// unpartitioned bucket (see ClearOrigin's docs), and there is no
// Storage.clearDataForStorageKey-equivalent that can be driven without
// first loading the flagged page to obtain a frameId — which this project
// never does. Driving this page's own JS data model directly is the only
// way to reach it without violating that rule: chrome://settings/content/
// all is a local, static Chrome page, not the flagged origin itself.
//
// Unlike ClearOrigin, there is no per-storage-type selection here — Chrome
// clears everything for the group in one shot, with no finer granularity
// offered by this primitive.
func ClearSiteGroup(ctx context.Context, client *cdp.Client, groupingKey string) error {
	sessionID, cleanup, err := cdp.AttachTarget(ctx, client, allSitesURL, false)
	if err != nil {
		return fmt.Errorf("removal: %w", err)
	}
	defer cleanup()

	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return ctx.Err()
	}

	key, err := json.Marshal(groupingKey)
	if err != nil {
		return fmt.Errorf("removal: encode grouping key: %w", err)
	}

	script := fmt.Sprintf(clearSiteGroupScript, key)

	var evalResult struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := client.CallSession(ctx, sessionID, "Runtime.evaluate", map[string]any{
		"expression":    script,
		"returnByValue": true,
	}, &evalResult); err != nil {
		return fmt.Errorf("removal: clear site group %s: %w", groupingKey, err)
	}
	if evalResult.ExceptionDetails != nil {
		return fmt.Errorf("removal: clear site group %s: %s", groupingKey, evalResult.ExceptionDetails.Text)
	}

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(evalResult.Result.Value), &payload); err != nil {
		return fmt.Errorf("removal: decode clear-site-group result for %s: %w", groupingKey, err)
	}
	if payload.Error != "" {
		return fmt.Errorf("removal: clear site group %s: %s", groupingKey, payload.Error)
	}
	return nil
}

// allSitesURL matches scan.allSitesURL — kept as a separate constant since
// the two packages don't share internals, but they must stay in sync (both
// name the same local Chrome page).
const allSitesURL = "chrome://settings/content/all"

// clearSiteGroupScript locates the <all-sites> WebUI element (the same one
// scan.DiscoverSiteData reads filteredList_ from) piercing shadow roots to
// find it, and calls its underlying browserProxy.clearSiteGroupDataAndCookies
// directly — the exact chrome.send("clearSiteGroupDataAndCookies", ...) IPC
// its own trash-icon click triggers, bypassing the confirmation-dialog UI
// (chrome-polish supplies its own confirm dialog instead) and the closed
// shadow root on its cr-input/cr-search-field descendants (irrelevant here
// since this never touches those elements — browserProxy is a plain JS
// property on <all-sites> itself, not behind any shadow boundary).
const clearSiteGroupScript = `
(function() {
  function findByTag(root, tag) {
    if (root.tagName && root.tagName.toLowerCase() === tag) return root;
    var kids = root.shadowRoot ? Array.from(root.shadowRoot.children).concat(Array.from(root.children)) : Array.from(root.children || []);
    for (var i = 0; i < kids.length; i++) {
      var r = findByTag(kids[i], tag);
      if (r) return r;
    }
    return null;
  }
  var el = findByTag(document.documentElement, 'all-sites');
  if (!el) return JSON.stringify({error: 'all-sites element not found'});
  if (!el.browserProxy || typeof el.browserProxy.clearSiteGroupDataAndCookies !== 'function') {
    return JSON.stringify({error: 'clearSiteGroupDataAndCookies not available on this Chrome version'});
  }
  el.browserProxy.clearSiteGroupDataAndCookies(%s);
  return JSON.stringify({ok: true});
})()
`

// Result is declared in removal.go; SiteGroupResult mirrors it for
// ClearSiteGroups' batch outcome, keyed by GroupingKey instead of Origin.
type SiteGroupResult struct {
	GroupingKey string
	Err         error
}

// ClearSiteGroups clears each of groupingKeys in turn over the same
// client, continuing past individual failures for the same reason
// ClearOrigins does.
func ClearSiteGroups(ctx context.Context, client *cdp.Client, groupingKeys []string) []SiteGroupResult {
	results := make([]SiteGroupResult, len(groupingKeys))
	for i, key := range groupingKeys {
		results[i] = SiteGroupResult{GroupingKey: key, Err: ClearSiteGroup(ctx, client, key)}
	}
	return results
}
