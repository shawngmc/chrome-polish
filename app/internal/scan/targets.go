package scan

import (
	"context"
	"fmt"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

// attachTarget opens a throwaway target navigated to targetURL and attaches
// a flattened session to it, returning the sessionID and a cleanup
// function that detaches and closes the target. Some CDP domains
// (ServiceWorker, and DOM/Runtime access to a specific document) are only
// available within a target session, not on the browser-level connection.
//
// background controls whether the new target is created in the background
// (false brings it to the foreground, briefly switching the person's
// active tab). Some Chrome WebUI pages use virtualized lists (iron-list)
// that only render rows once they've actually been laid out, which
// background tabs don't get — so a background target reads back empty for
// those pages even though the underlying data exists.
//
// Callers must only ever pass a local, static Chrome URL here (e.g.
// "about:blank" or a chrome://settings/... page) — never a
// candidate/suspect origin.
func attachTarget(ctx context.Context, client *cdp.Client, targetURL string, background bool) (sessionID string, cleanup func(), err error) {
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := client.Call(ctx, "Target.createTarget", map[string]any{
		"url":        targetURL,
		"background": background,
	}, &created); err != nil {
		return "", nil, fmt.Errorf("scan: Target.createTarget: %w", err)
	}

	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := client.Call(ctx, "Target.attachToTarget", map[string]any{
		"targetId": created.TargetID,
		"flatten":  true,
	}, &attached); err != nil {
		_ = client.Call(context.Background(), "Target.closeTarget", map[string]any{"targetId": created.TargetID}, nil)
		return "", nil, fmt.Errorf("scan: Target.attachToTarget: %w", err)
	}

	cleanup = func() {
		client.Call(context.Background(), "Target.closeTarget", map[string]any{"targetId": created.TargetID}, nil)
	}

	return attached.SessionID, cleanup, nil
}
