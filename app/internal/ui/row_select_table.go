package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// rowSelectTable adapts widget.Table's per-cell keyboard handling into
// whole-row navigation for OriginsPanel's origins table. widget.Table
// itself only tracks a single highlighted/selected cell and moves it one
// cell at a time (including sideways, with Left/Right); this wraps it so
// Up/Down/PageUp/PageDown/Home/End move which row is focused instead, and
// Space toggles that row's selection checkbox rather than re-selecting
// the highlighted cell. Mouse interaction (tapping a cell) is left to the
// embedded Table unchanged — OriginsPanel.showDetail only looks at the
// row anyway, so a click on any cell already behaves like a row click.
type rowSelectTable struct {
	*widget.Table
	panel *OriginsPanel
}

// newRowSelectTable builds a rowSelectTable the same way widget.NewTable
// builds a *widget.Table, plus the OriginsPanel it drives keyboard
// navigation against.
func newRowSelectTable(panel *OriginsPanel, length func() (int, int), create func() fyne.CanvasObject, update func(widget.TableCellID, fyne.CanvasObject)) *rowSelectTable {
	return &rowSelectTable{
		Table: widget.NewTable(length, create, update),
		panel: panel,
	}
}

// Tapped fixes a focus bug that would otherwise make TypedKey below
// unreachable: widget.BaseWidget.ExtendBaseWidget records whichever
// object first calls it as the widget's permanent "super" — for the
// embedded *widget.Table that's itself (set inside widget.NewTable,
// before rowSelectTable ever exists to wrap it), and that binding can't
// be changed afterward. widget.Table's own Tapped focuses the canvas on
// exactly that frozen super, i.e. the raw embedded Table, not whichever
// object the click actually landed on — so a plain embed-and-override of
// TypedKey alone would never receive key events: the canvas would hold
// keyboard focus on the inner Table (running its original, unmodified
// TypedKey) no matter what rowSelectTable defines. Running the embedded
// Table's Tapped first (for its normal cell-resolution/Select/OnSelected
// side effects) and then explicitly refocusing the canvas onto t (this
// wrapper) corrects that, so subsequent key events reach TypedKey below.
func (t *rowSelectTable) Tapped(e *fyne.PointEvent) {
	t.Table.Tapped(e)
	if c := fyne.CurrentApp().Driver().CanvasForObject(t); c != nil {
		c.Focus(t)
	}
}

// TypedKey overrides widget.Table's TypedKey: Left/Right are ignored
// (there's no column to move between in row-selection mode), Space
// toggles the focused row's checkbox instead of calling the embedded
// Table's cell Select, and PageUp/PageDown/Home/End — which widget.Table
// doesn't handle at all — jump the row focus accordingly.
func (t *rowSelectTable) TypedKey(event *fyne.KeyEvent) {
	p := t.panel
	switch event.Name {
	case fyne.KeyUp:
		p.moveRowFocus(-1)
	case fyne.KeyDown:
		p.moveRowFocus(1)
	case fyne.KeyPageUp:
		p.moveRowFocus(-pageRowStep)
	case fyne.KeyPageDown:
		p.moveRowFocus(pageRowStep)
	case fyne.KeyHome:
		p.setRowFocus(0)
	case fyne.KeyEnd:
		p.setRowFocus(len(p.visible) - 1)
	case fyne.KeySpace:
		p.toggleFocusedRow()
	}
}
