package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

// TestBeginOpDisablesAllActionButtons guards against the race the busy
// indicator diff introduced: five independently-clickable buttons
// (Scan/Check Permissions/Refresh/Remove/Deep Clean) share one client, so an
// in-flight operation must disable all of them, not just its own.
func TestBeginOpDisablesAllActionButtons(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	p := NewOriginsPanel(win)
	p.simpleMode = false // avoid SetClient's simple-mode auto-refresh spawning a goroutine against the fake client
	p.SetClient(&cdp.Client{})

	p.beginOp("working...")

	for name, btn := range map[string]interface{ Disabled() bool }{
		"scanBtn":      p.scanBtn,
		"permsBtn":     p.permsBtn,
		"refreshBtn":   p.refreshBtn,
		"removeBtn":    p.removeBtn,
		"deepCleanBtn": p.deepCleanBtn,
	} {
		if !btn.Disabled() {
			t.Errorf("%s should be disabled once an operation is in flight", name)
		}
	}
	if !p.busy.Visible() {
		t.Error("busy indicator should be visible once an operation is in flight")
	}
}

// TestEndOpStaleGenerationIsNoOp verifies the fix for the stale-reconnect
// bug: a completion from an operation superseded by a SetClient call (e.g. a
// disconnect/reconnect while it was still running) must not clobber the
// newer connection's button/busy state.
func TestEndOpStaleGenerationIsNoOp(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	p := NewOriginsPanel(win)
	p.simpleMode = false // avoid SetClient's simple-mode auto-refresh spawning a goroutine against the fake client
	p.SetClient(&cdp.Client{})

	staleGen := p.beginOp("scanning with the old client...")

	// Supersede it: disconnect, then reconnect with a new client, as a rapid
	// disconnect/reconnect would in the real UI.
	p.SetClient(nil)
	p.SetClient(&cdp.Client{})

	// The stale operation's goroutine finally completes and calls endOp.
	p.endOp(staleGen)

	if p.scanBtn.Disabled() {
		t.Error("endOp with a stale generation re-enabled scanBtn, clobbering the new connection's state")
	}
	if p.busy.Visible() {
		t.Error("endOp with a stale generation left the busy indicator visible")
	}

	// The current, non-stale operation still completes normally.
	currentGen := p.beginOp("scanning with the new client...")
	p.endOp(currentGen)
	if p.scanBtn.Disabled() {
		t.Error("endOp with the current generation should re-enable scanBtn")
	}
	if p.busy.Visible() {
		t.Error("endOp with the current generation should hide the busy indicator")
	}
}
