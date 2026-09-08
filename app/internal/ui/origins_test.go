package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
	"github.com/shawngmc/chrome-polish/app/internal/scan"
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

// TestRowSelectTableKeyboardNavigation exercises rowSelectTable's
// row-based keyboard handling directly (bypassing focus/mouse plumbing,
// which test.Window doesn't simulate): Up/Down/Home/End/PageUp move
// focusedRow across whole rows rather than widget.Table's native
// per-cell movement, and Space toggles the focused row's selection
// checkbox rather than re-selecting a cell.
func TestRowSelectTableKeyboardNavigation(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	p := NewOriginsPanel(win)

	p.origins = []scan.Origin{
		{Origin: "https://a.example"},
		{Origin: "https://b.example"},
		{Origin: "https://c.example"},
	}
	p.visible = p.origins

	if p.focusedRow != -1 {
		t.Fatalf("focusedRow should start at -1 (nothing focused), got %d", p.focusedRow)
	}

	p.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	if p.focusedRow != 0 {
		t.Fatalf("first Down should focus row 0, got %d", p.focusedRow)
	}

	p.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	if p.focusedRow != 1 {
		t.Fatalf("second Down should focus row 1, got %d", p.focusedRow)
	}

	p.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeyUp})
	if p.focusedRow != 0 {
		t.Fatalf("Up should move focus back to row 0, got %d", p.focusedRow)
	}

	p.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnd})
	if p.focusedRow != 2 {
		t.Fatalf("End should focus the last row (2), got %d", p.focusedRow)
	}

	p.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeyPageUp})
	if p.focusedRow != 0 {
		t.Fatalf("PageUp past the top should clamp to row 0, got %d", p.focusedRow)
	}

	p.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeyPageDown})
	if p.focusedRow != 2 {
		t.Fatalf("PageDown past the bottom should clamp to the last row (2), got %d", p.focusedRow)
	}

	p.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeyHome})
	if p.focusedRow != 0 {
		t.Fatalf("Home should focus row 0, got %d", p.focusedRow)
	}

	p.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if !p.selected["https://a.example"] {
		t.Fatal("Space should check the focused row's selection box")
	}

	p.table.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if p.selected["https://a.example"] {
		t.Fatal("Space again should uncheck the focused row's selection box")
	}
}

// TestRowSelectTableTapGrantsFocusToWrapper guards against a real Fyne
// gotcha (see rowSelectTable.Tapped's doc comment): widget.Table freezes
// its BaseWidget "super" reference to itself at construction, before
// rowSelectTable ever wraps it, so widget.Table's own Tapped focuses the
// canvas on the raw embedded Table rather than on whatever object the
// tap actually landed on. Embedding *widget.Table and overriding TypedKey
// alone compiles fine and passes the previous test (which calls TypedKey
// directly, bypassing focus entirely) — but in the real app, a click on
// the table would leave the canvas focused on the inner Table, so its
// original per-cell key handling would run instead of rowSelectTable's,
// and every key binding in this file would silently do nothing. This
// drives an actual tap through test.Tap (which exercises the same
// focus-on-tap path as a real click) and checks focus lands on the
// wrapper, then dispatches a key exactly the way the canvas would: via
// whatever object canvas.Focused() actually returns.
func TestRowSelectTableTapGrantsFocusToWrapper(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	p := NewOriginsPanel(win)
	win.SetContent(p.Results())
	win.Resize(fyne.NewSize(900, 600))

	p.origins = []scan.Origin{
		{Origin: "https://a.example"},
		{Origin: "https://b.example"},
	}
	p.visible = p.origins
	p.table.Refresh()

	test.Tap(p.table)

	focused := win.Canvas().Focused()
	if focused != fyne.Focusable(p.table) {
		t.Fatalf("tapping the table should focus the rowSelectTable wrapper (so it receives key events), got %#v", focused)
	}

	focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	if p.focusedRow != 0 {
		t.Fatalf("Down dispatched through the actually-focused object should move row focus, got focusedRow=%d", p.focusedRow)
	}
}
