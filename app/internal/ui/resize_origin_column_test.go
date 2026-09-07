package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/shawngmc/chrome-polish/app/internal/scan"
)

// TestResizeOriginColumnCachesMeasurements guards the perf fix: re-filtering
// an unchanged origin set (as every filter-box keystroke does) must not
// re-measure origins already seen, since fyne.MeasureText's font-shaping
// work is real cost on a busy profile (1000+ candidate origins isn't
// unusual — see cdp-remote-debugging-quirks item 6).
func TestResizeOriginColumnCachesMeasurements(t *testing.T) {
	win := test.NewWindow(nil)
	defer win.Close()
	p := NewOriginsPanel(win)

	p.origins = []scan.Origin{{Origin: "https://short.example"}, {Origin: "https://a-much-longer-hostname.example"}}
	p.visible = p.origins

	p.resizeOriginColumn()
	if got := len(p.originWidthCache); got != 2 {
		t.Fatalf("after first resize, cache has %d entries, want 2", got)
	}
	widthAfterFirst := p.originWidthCache["https://a-much-longer-hostname.example"]

	// Simulate the filter box narrowing, then clearing back to the full
	// set — same strings, seen again.
	p.visible = p.origins[:1]
	p.resizeOriginColumn()
	p.visible = p.origins
	p.resizeOriginColumn()

	if got := len(p.originWidthCache); got != 2 {
		t.Errorf("cache grew to %d entries on repeated filtering of the same origins, want still 2", got)
	}
	if got := p.originWidthCache["https://a-much-longer-hostname.example"]; got != widthAfterFirst {
		t.Errorf("cached width changed across calls: got %v, want %v", got, widthAfterFirst)
	}
}
