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

	for i, btn := range p.opButtons() {
		if !btn.Disabled() {
			t.Errorf("opButtons()[%d] should be disabled once an operation is in flight", i)
		}
	}
	if !p.busy.Visible() {
		t.Error("busy indicator should be visible once an operation is in flight")
	}
}

// TestEndOpStaleClientIsNoOp verifies the fix for the stale-reconnect bug: a
// completion from an operation superseded by a SetClient call (e.g. a
// disconnect/reconnect while it was still running) must not clobber the
// newer connection's button/busy state.
func TestEndOpStaleClientIsNoOp(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	p := NewOriginsPanel(win)
	p.simpleMode = false // avoid SetClient's simple-mode auto-refresh spawning a goroutine against the fake client
	staleClient := &cdp.Client{}
	p.SetClient(staleClient)

	p.beginOp("scanning with the old client...")

	// Supersede it: disconnect, then reconnect with a new client, as a rapid
	// disconnect/reconnect would in the real UI.
	p.SetClient(nil)
	p.SetClient(&cdp.Client{})

	// The stale operation's goroutine finally completes and calls endOp,
	// still holding the client it was launched with.
	if p.opCurrent(staleClient) {
		t.Fatal("staleClient should no longer be current after two more SetClient calls")
	}
	p.endOp(staleClient)

	if p.scanBtn.Disabled() {
		t.Error("endOp for a stale client re-enabled scanBtn, clobbering the new connection's state")
	}
	if p.busy.Visible() {
		t.Error("endOp for a stale client left the busy indicator visible")
	}

	// The current, non-stale operation still completes normally.
	p.beginOp("scanning with the new client...")
	p.endOp(p.client)
	if p.scanBtn.Disabled() {
		t.Error("endOp for the current client should re-enable scanBtn")
	}
	if p.busy.Visible() {
		t.Error("endOp for the current client should hide the busy indicator")
	}
}
