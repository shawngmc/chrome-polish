package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

const historyURL = "chrome://history"

// DefaultHistoryBudget bounds how long the in-page walk of lastVisitedScript
// keeps paging through history before it gives up and returns whatever it's
// aggregated so far (Truncated in lastVisitedPayload) — a profile with a
// very long history shouldn't be able to blow past the caller's context
// deadline with nothing to show for it at all.
const DefaultHistoryBudget = 20 * time.Second

// lastVisitedScript drives chrome://history's own <history-query-manager>
// element through its full result set and reduces it to one
// most-recent-visit timestamp per origin, entirely in-page.
//
// There's no CDP domain for browsing history (unlike cookies/service
// workers) and no per-origin summary view of it anywhere in Chrome — the
// only source is the same paginated, per-visit query the chrome://history
// UI itself drives (see DiscoverLastVisited's doc comment for how this was
// confirmed against a live profile). Reducing to per-origin max-timestamp
// inside the page, rather than shipping every visit back over the CDP
// link, keeps the round trip small and never surfaces individual page
// titles/URLs outside the browser at all.
//
// el.queryHistory_ and el.queryState/el.queryResult are "private" only by
// underscore convention (same as all-sites' filteredList_ in sitedata.go) —
// ordinary accessible JS properties on the custom element, not a shadow
// boundary being crossed. queryHistory_(true) requests the next page
// (BrowserProxyImpl.getInstance().handler.queryHistoryContinuation()) and
// resolves by *replacing* queryResult.value with just that page (confirmed
// by reading onQueryResult_'s own source), so each page must be ingested as
// it arrives rather than read once at the end.
const lastVisitedScript = `
(async function(budgetMs) {
` + findByTagScript + `
  // Polls for history-query-manager rather than assuming the caller waited
  // long enough after navigation: faster than a flat pre-sleep when the
  // page is already up, and more robust than one when it isn't (a slow
  // profile/machine gets extra time instead of a hard failure). Waits for
  // queryState/queryResult specifically, not just the element itself — on
  // a large/slow profile the custom element can be upgraded and present
  // in the DOM before those observables are initialized, and reading them
  // too early throws.
  function waitForEl(tag, timeoutMs) {
    return new Promise(function(resolve) {
      var start = Date.now();
      (function check() {
        var el = findByTag(document.documentElement, tag);
        if (el && el.queryState && el.queryResult) return resolve(el);
        if (Date.now() - start > timeoutMs) return resolve(null);
        setTimeout(check, 50);
      })();
    });
  }

  // Runtime.evaluate's handling of a rejected awaited promise doesn't
  // reliably surface as exceptionDetails on the Go side — a stray
  // exception here can come back as a non-string result.value that fails
  // to decode entirely. Converting any unexpected failure into the same
  // {error: ...} shape as a known failure (history-query-manager not
  // found) keeps every failure mode reaching Go as valid, decodable JSON.
  try {
    var el = await waitForEl('history-query-manager', 5000);
    if (!el) return JSON.stringify({error: 'history-query-manager not found'});

    var waitIdle = function() {
      return new Promise(function(resolve) {
        (function check() {
          if (!el.queryState.querying) resolve();
          else setTimeout(check, 15);
        })();
      });
    };

    var lastVisit = {};
    var ingest = function(entries) {
      for (var i = 0; i < entries.length; i++) {
        var e = entries[i];
        var origin;
        try { origin = new URL(e.url).origin; } catch (err) { continue; }
        if (!(origin in lastVisit) || e.time > lastVisit[origin]) lastVisit[origin] = e.time;
      }
    };

    var start = Date.now();
    await waitIdle();
    ingest(el.queryResult.value || []);

    var truncated = false;
    while (!el.queryResult.info.finished) {
      if (Date.now() - start > budgetMs) {
        truncated = true;
        break;
      }
      el.queryHistory_(true);
      await waitIdle();
      ingest(el.queryResult.value || []);
    }

    return JSON.stringify({lastVisit: lastVisit, truncated: truncated});
  } catch (err) {
    return JSON.stringify({error: String((err && err.message) || err)});
  }
})(%d)
`

type lastVisitedPayload struct {
	Error     string             `json:"error"`
	LastVisit map[string]float64 `json:"lastVisit"`
	Truncated bool               `json:"truncated"`
}

// DiscoverLastVisited reads every origin's most recent visit time from
// chrome://history — a local, static Chrome page, never a candidate/suspect
// origin — the only signal in Chrome that tracks actual navigation history
// (chrome://site-engagement, the other plausible source, was checked
// live against a real profile and carries no timestamp at all: its data
// model is just origin/baseScore/installedBonus/totalScore).
//
// budget caps how long the in-page walk (lastVisitedScript) pages through
// history before returning whatever it's gathered so far; pass 0 to use
// DefaultHistoryBudget. Truncation on a very large history is reported as a
// nil error with a partial map, not a failure — DiscoverSiteData's
// graceful-degradation precedent, not an all-or-nothing read.
func DiscoverLastVisited(ctx context.Context, client *cdp.Client, budget time.Duration) (map[string]time.Time, error) {
	if budget <= 0 {
		budget = DefaultHistoryBudget
	}

	sessionID, cleanup, err := cdp.AttachTarget(ctx, client, historyURL, false)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	defer cleanup()

	script := fmt.Sprintf(lastVisitedScript, budget.Milliseconds())

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
		"awaitPromise":  true,
	}, &evalResult); err != nil {
		return nil, fmt.Errorf("scan: Runtime.evaluate on %s: %w", historyURL, err)
	}
	if evalResult.ExceptionDetails != nil {
		return nil, fmt.Errorf("scan: script threw on %s: %s", historyURL, evalResult.ExceptionDetails.Text)
	}

	var payload lastVisitedPayload
	if err := json.Unmarshal([]byte(evalResult.Result.Value), &payload); err != nil {
		return nil, fmt.Errorf("scan: decode last-visited data: %w", err)
	}
	if payload.Error != "" {
		return nil, fmt.Errorf("scan: %s", payload.Error)
	}

	lastVisited := make(map[string]time.Time, len(payload.LastVisit))
	for origin, ms := range payload.LastVisit {
		lastVisited[origin] = time.UnixMilli(int64(ms))
	}
	return lastVisited, nil
}
