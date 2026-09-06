package ui

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
	"github.com/shawngmc/chrome-polish/app/internal/removal"
	"github.com/shawngmc/chrome-polish/app/internal/reputation"
	"github.com/shawngmc/chrome-polish/app/internal/scan"
)

const (
	scanTimeout      = 15 * time.Second
	permissionsCheck = "Check permissions"
)

// origins table columns.
const (
	colSelect = iota
	colOrigin
	colScore
	colBlocklisted
	colCookie
	colServiceWorker
	colNotifications
	colCamera
	colMicrophone
	numCols
)

var columnTitles = [numCols]string{
	colOrigin:        "Origin",
	colScore:         "Score",
	colBlocklisted:   "Blocklisted",
	colCookie:        "Cookie",
	colServiceWorker: "Service Worker",
	colNotifications: "Notifications",
	colCamera:        "Camera",
	colMicrophone:    "Microphone",
}

var columnCategory = [numCols]string{
	colNotifications: scan.CategoryNotifications.Category,
	colCamera:        scan.CategoryCamera.Category,
	colMicrophone:    scan.CategoryMicrophone.Category,
}

// OriginsPanel runs origin discovery, permission-state reads, and
// reputation scoring (DESIGN.md sections 4.1, 4.2, and 4.4) against a
// connected client and lists the results in a sortable table, with a
// detail pane showing why the selected origin was scored the way it was.
// Discovery and permission reads reuse whatever client is attached — no
// new websocket connection, and so no extra Chrome connection approval
// beyond the one already made to connect. Scoring is a pure function over
// already-collected data and needs no connection at all.
type OriginsPanel struct {
	controls fyne.CanvasObject
	results  fyne.CanvasObject
	win      fyne.Window

	client *cdp.Client

	scanBtn      *widget.Button
	permsBtn     *widget.Button
	blocklistBtn *widget.Button
	optionsBtn   *widget.Button
	removeBtn    *widget.Button
	filterEntry  *widget.Entry
	status       *widget.Label
	detail       *widget.Label
	table        *widget.Table

	origins     []scan.Origin                               // full discovered set, in current sort order
	visible     []scan.Origin                               // origins after the filter box, what the table renders
	filter      string                                      // lowercased, from filterEntry
	permissions map[string]map[string]scan.PermissionStatus // origin -> category -> status
	scores      map[string]reputation.Score                 // origin -> score
	blocklist   reputation.Blocklist
	selected    map[string]bool // origin -> checked, for bulk removal

	sortCol        int // -1 if unsorted
	sortAsc        bool
	colorBlindMode bool
}

// NewOriginsPanel builds a ready-to-use origins panel. It starts with no
// client attached; call SetClient once a connection is established. win is
// used to anchor the blocklist file-picker dialog.
func NewOriginsPanel(win fyne.Window) *OriginsPanel {
	p := &OriginsPanel{
		win:         win,
		permissions: make(map[string]map[string]scan.PermissionStatus),
		scores:      make(map[string]reputation.Score),
		selected:    make(map[string]bool),
		sortCol:     -1,
	}

	p.status = widget.NewLabel("Connect first, then scan for candidate origins.")
	p.status.Wrapping = fyne.TextWrapWord

	p.detail = widget.NewLabel("Select a row to see why it was scored that way.")
	p.detail.Wrapping = fyne.TextWrapWord

	p.table = widget.NewTable(
		func() (int, int) { return len(p.visible), numCols },
		func() fyne.CanvasObject {
			bg := canvas.NewRectangle(color.Transparent)
			label := widget.NewLabel("")
			label.Truncation = fyne.TextTruncateEllipsis
			check := widget.NewCheck("", nil)
			return container.NewStack(bg, label, check)
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*fyne.Container)
			bg := cell.Objects[0].(*canvas.Rectangle)
			label := cell.Objects[1].(*widget.Label)
			check := cell.Objects[2].(*widget.Check)

			origin := p.visible[id.Row]

			bg.FillColor = color.Transparent
			if id.Col == colScore {
				bg.FillColor = p.scoreColor(p.scores[origin.Origin].Total)
			}
			bg.Refresh()

			if id.Col == colSelect {
				label.Hide()
				// Clear the (possibly stale, reused-from-another-row)
				// callback before syncing state, so SetChecked can't fire
				// a handler still capturing a different origin.
				check.OnChanged = nil
				check.SetChecked(p.selected[origin.Origin])
				o := origin.Origin
				check.OnChanged = func(checked bool) { p.onToggleSelect(o, checked) }
				check.Show()
				return
			}

			check.Hide()
			label.SetText(p.cellText(origin, id.Col))
			label.Show()
		},
	)
	p.table.ShowHeaderRow = true
	p.table.CreateHeader = func() fyne.CanvasObject {
		return container.NewStack(widget.NewButton("", nil), widget.NewCheck("", nil))
	}
	p.table.UpdateHeader = func(id widget.TableCellID, obj fyne.CanvasObject) {
		cell := obj.(*fyne.Container)
		btn := cell.Objects[0].(*widget.Button)
		check := cell.Objects[1].(*widget.Check)

		if id.Col == colSelect {
			btn.Hide()
			check.OnChanged = nil
			check.SetChecked(p.allVisibleSelected())
			check.OnChanged = p.onToggleSelectAll
			check.Show()
			return
		}

		check.Hide()
		btn.SetText(p.headerText(id.Col))
		col := id.Col
		btn.OnTapped = func() { p.onSortColumn(col) }
		btn.Show()
	}
	p.table.OnSelected = func(id widget.TableCellID) { p.showDetail(id.Row) }
	p.table.SetColumnWidth(colSelect, 40)
	p.table.SetColumnWidth(colOrigin, 280)
	p.table.SetColumnWidth(colScore, 70)
	p.table.SetColumnWidth(colBlocklisted, 100)
	p.table.SetColumnWidth(colCookie, 80)
	p.table.SetColumnWidth(colServiceWorker, 130)
	p.table.SetColumnWidth(colNotifications, 130)
	p.table.SetColumnWidth(colCamera, 100)
	p.table.SetColumnWidth(colMicrophone, 110)

	p.scanBtn = widget.NewButton("Scan for origins", p.onScan)
	p.scanBtn.Disable()

	p.permsBtn = widget.NewButton(permissionsCheck, p.onCheckPermissions)
	p.permsBtn.Disable()

	p.blocklistBtn = widget.NewButton("Load blocklist...", p.onLoadBlocklist)

	p.optionsBtn = widget.NewButton("Options...", p.onOptions)

	p.removeBtn = widget.NewButton("Remove data...", p.onRemove)
	p.removeBtn.Importance = widget.DangerImportance
	p.removeBtn.Disable()

	p.filterEntry = widget.NewEntry()
	p.filterEntry.SetPlaceHolder("Filter by domain...")
	p.filterEntry.OnChanged = p.onFilterChanged

	p.controls = container.NewVBox(
		p.scanBtn, p.permsBtn, p.blocklistBtn, p.optionsBtn,
		widget.NewSeparator(),
		p.status,
	)

	p.results = container.NewBorder(
		container.NewBorder(nil, nil, widget.NewLabel("Filter:"), nil, p.filterEntry),
		container.NewVBox(p.detail, p.removeBtn),
		nil, nil,
		p.table,
	)

	return p
}

// Controls returns the panel's action buttons and status label, meant for
// a sidebar alongside the connection controls.
func (p *OriginsPanel) Controls() fyne.CanvasObject {
	return p.controls
}

// Results returns the panel's filter box, table, and detail pane, meant
// for the main content area.
func (p *OriginsPanel) Results() fyne.CanvasObject {
	return p.results
}

func (p *OriginsPanel) cellText(origin scan.Origin, col int) string {
	switch col {
	case colOrigin:
		return origin.Origin
	case colScore:
		return strconv.Itoa(p.scores[origin.Origin].Total)
	case colBlocklisted:
		return checkmark(p.isBlocklisted(origin.Origin))
	case colCookie:
		return checkmark(hasSource(origin.Sources, scan.SourceCookie))
	case colServiceWorker:
		return checkmark(hasSource(origin.Sources, scan.SourceServiceWorker))
	case colNotifications, colCamera, colMicrophone:
		return string(p.permissionStatus(origin.Origin, columnCategory[col]))
	default:
		return ""
	}
}

func (p *OriginsPanel) headerText(col int) string {
	title := columnTitles[col]
	if p.sortCol != col {
		return title
	}
	if p.sortAsc {
		return title + " ▲"
	}
	return title + " ▼"
}

func checkmark(b bool) string {
	if b {
		return "✓"
	}
	return ""
}

func hasSource(sources []scan.Source, want scan.Source) bool {
	for _, s := range sources {
		if s == want {
			return true
		}
	}
	return false
}

func (p *OriginsPanel) permissionStatus(origin, category string) scan.PermissionStatus {
	return p.permissions[origin][category]
}

func (p *OriginsPanel) isBlocklisted(origin string) bool {
	for _, s := range p.scores[origin].Signals {
		if s.Name == reputation.SignalBlocklist {
			return true
		}
	}
	return false
}

func (p *OriginsPanel) onToggleSelect(origin string, checked bool) {
	if checked {
		p.selected[origin] = true
	} else {
		delete(p.selected, origin)
	}
	p.updateRemoveButton()
	p.table.Refresh() // so the header "select all" checkbox reflects the change
}

func (p *OriginsPanel) onToggleSelectAll(checked bool) {
	for _, o := range p.visible {
		if checked {
			p.selected[o.Origin] = true
		} else {
			delete(p.selected, o.Origin)
		}
	}
	p.updateRemoveButton()
	p.table.Refresh()
}

func (p *OriginsPanel) allVisibleSelected() bool {
	if len(p.visible) == 0 {
		return false
	}
	for _, o := range p.visible {
		if !p.selected[o.Origin] {
			return false
		}
	}
	return true
}

// updateRemoveButton syncs the Remove button's label and enabled state to
// the current selection count.
func (p *OriginsPanel) updateRemoveButton() {
	n := len(p.selected)
	if n == 0 {
		p.removeBtn.SetText("Remove data...")
		p.removeBtn.Disable()
		return
	}
	p.removeBtn.SetText(fmt.Sprintf("Remove data (%d)...", n))
	if p.client != nil {
		p.removeBtn.Enable()
	}
}

func (p *OriginsPanel) countBlocklisted() int {
	n := 0
	for _, o := range p.origins {
		if p.isBlocklisted(o.Origin) {
			n++
		}
	}
	return n
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func (p *OriginsPanel) onFilterChanged(text string) {
	p.filter = strings.ToLower(strings.TrimSpace(text))
	p.applyFilter()
	p.table.Refresh()
}

// applyFilter rebuilds visible from origins (which stays in whatever sort
// order sortOrigins last put it in), keeping only origins whose hostname
// contains the filter text.
func (p *OriginsPanel) applyFilter() {
	if p.filter == "" {
		p.visible = p.origins
	} else {
		visible := make([]scan.Origin, 0, len(p.origins))
		for _, o := range p.origins {
			if strings.Contains(strings.ToLower(o.Origin), p.filter) {
				visible = append(visible, o)
			}
		}
		p.visible = visible
	}
	p.resizeOriginColumn()
}

// resizeOriginColumn sizes the Origin column to fit the longest origin
// currently visible, so hostnames aren't clipped, within sane bounds so a
// single very long entry can't push the column absurdly wide.
func (p *OriginsPanel) resizeOriginColumn() {
	const minWidth, maxWidth = float32(150), float32(600)

	textSize := theme.TextSize()
	widest := float32(0)
	for _, o := range p.visible {
		w := fyne.MeasureText(o.Origin, textSize, fyne.TextStyle{}).Width
		if w > widest {
			widest = w
		}
	}

	width := widest + theme.Padding()*4
	switch {
	case width < minWidth:
		width = minWidth
	case width > maxWidth:
		width = maxWidth
	}
	p.table.SetColumnWidth(colOrigin, width)
}

// SetClient attaches (or, with nil, detaches) the connected client this
// panel operates against, enabling or disabling its buttons to match, and
// clearing any previous results. The loaded blocklist, if any, is kept —
// it's operator configuration, not per-session scan state.
func (p *OriginsPanel) SetClient(client *cdp.Client) {
	p.client = client
	p.origins = nil
	p.visible = nil
	p.permissions = make(map[string]map[string]scan.PermissionStatus)
	p.scores = make(map[string]reputation.Score)
	p.sortCol = -1
	p.selected = make(map[string]bool)
	p.detail.SetText("Select a row to see why it was scored that way.")
	p.filterEntry.SetText("") // triggers onFilterChanged -> applyFilter
	p.updateRemoveButton()
	p.table.Refresh()

	if client != nil {
		p.scanBtn.Enable()
		p.permsBtn.Enable()
		p.status.SetText("Connected. Ready to scan.")
	} else {
		p.scanBtn.Disable()
		p.permsBtn.Disable()
		p.status.SetText("Connect first, then scan for candidate origins.")
	}
}

func (p *OriginsPanel) onSortColumn(col int) {
	if p.sortCol == col {
		p.sortAsc = !p.sortAsc
	} else {
		p.sortCol = col
		// Highest score first reads more naturally than lowest-first on
		// the initial click; every other column defaults to ascending.
		p.sortAsc = col != colScore
	}
	p.sortOrigins()
	p.applyFilter()
	p.table.Refresh()
}

func (p *OriginsPanel) sortOrigins() {
	if p.sortCol < 0 {
		return
	}
	sort.SliceStable(p.origins, func(i, j int) bool {
		a, b := p.origins[i], p.origins[j]
		if !p.sortAsc {
			a, b = b, a
		}
		return p.less(a, b)
	})
}

func (p *OriginsPanel) less(a, b scan.Origin) bool {
	if p.sortCol == colScore {
		return p.scores[a.Origin].Total < p.scores[b.Origin].Total
	}
	return p.sortKey(a) < p.sortKey(b)
}

func (p *OriginsPanel) sortKey(o scan.Origin) string {
	switch p.sortCol {
	case colOrigin:
		return o.Origin
	case colBlocklisted:
		return boolKey(p.isBlocklisted(o.Origin))
	case colCookie:
		return boolKey(hasSource(o.Sources, scan.SourceCookie))
	case colServiceWorker:
		return boolKey(hasSource(o.Sources, scan.SourceServiceWorker))
	case colNotifications, colCamera, colMicrophone:
		return string(p.permissionStatus(o.Origin, columnCategory[p.sortCol]))
	default:
		return ""
	}
}

func boolKey(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// recomputeScores runs reputation scoring over the current origins and
// permissions and refreshes the table. If no column has been explicitly
// sorted yet, it defaults to highest-score-first so the most interesting
// rows surface immediately.
func (p *OriginsPanel) recomputeScores() {
	scores := reputation.ComputeAll(p.origins, p.permissions, p.blocklist)
	p.scores = make(map[string]reputation.Score, len(scores))
	for _, s := range scores {
		p.scores[s.Origin] = s
	}

	if p.sortCol < 0 {
		p.sortCol = colScore
		p.sortAsc = false
	}
	p.sortOrigins()
	p.applyFilter()
	p.table.Refresh()
}

func (p *OriginsPanel) showDetail(row int) {
	if row < 0 || row >= len(p.visible) {
		return
	}
	origin := p.visible[row]
	score := p.scores[origin.Origin]

	if len(score.Signals) == 0 {
		p.detail.SetText(fmt.Sprintf("%s — score 0. No signals matched.", origin.Origin))
		return
	}

	lines := make([]string, len(score.Signals))
	for i, s := range score.Signals {
		lines[i] = fmt.Sprintf("+%d %s", s.Weight, s.Detail)
	}
	p.detail.SetText(fmt.Sprintf("%s — score %d\n%s", origin.Origin, score.Total, strings.Join(lines, "\n")))
}

// scoreColor maps a score to a color on the current palette (the default
// green/yellow/orange/red scale, or the color-blind-safe blue/yellow/
// orange/purple scale — see score_color.go).
func (p *OriginsPanel) scoreColor(score int) color.Color {
	stops := scoreColorStops
	if p.colorBlindMode {
		stops = scoreColorStopsColorBlind
	}
	return scoreColor(stops, score)
}

func (p *OriginsPanel) onOptions() {
	check := widget.NewCheck("Color-blind friendly score colors", func(checked bool) {
		p.colorBlindMode = checked
		p.table.Refresh()
	})
	check.SetChecked(p.colorBlindMode)
	dialog.ShowCustom("Options", "Close", check, p.win)
}

func (p *OriginsPanel) onLoadBlocklist() {
	dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, p.win)
			return
		}
		if reader == nil {
			return // cancelled
		}
		defer reader.Close()

		data, err := io.ReadAll(reader)
		if err != nil {
			dialog.ShowError(err, p.win)
			return
		}

		p.blocklist = reputation.ParseBlocklist(data)
		n := len(p.blocklist)
		msg := fmt.Sprintf("Loaded %d blocklist %s.", n, pluralize(n, "entry", "entries"))

		if len(p.origins) > 0 {
			p.recomputeScores()
			matched := p.countBlocklisted()
			msg += fmt.Sprintf(" %d of %d scanned origin(s) matched.", matched, len(p.origins))
		} else {
			msg += " Scan for origins to check them against it."
		}
		p.status.SetText(msg)
	}, p.win)
}

// onRemove opens a confirmation dialog for clearing the checked origins'
// data, letting the person pick exactly which storage types to clear
// (DESIGN.md section 4.3: precise, per-type removal, never a blanket
// wipe) before anything actually happens.
func (p *OriginsPanel) onRemove() {
	if len(p.selected) == 0 || p.client == nil {
		return
	}

	origins := make([]string, 0, len(p.selected))
	for o := range p.selected {
		origins = append(origins, o)
	}
	sort.Strings(origins)

	defaultChecked := make(map[removal.StorageType]bool, len(removal.DefaultStorageTypes))
	for _, t := range removal.DefaultStorageTypes {
		defaultChecked[t] = true
	}

	checks := make(map[removal.StorageType]*widget.Check, len(removal.AllStorageTypes))
	typeList := container.NewVBox()
	for _, t := range removal.AllStorageTypes {
		check := widget.NewCheck(string(t), nil)
		check.SetChecked(defaultChecked[t])
		checks[t] = check
		typeList.Add(check)
	}

	const previewLimit = 10
	preview := origins
	suffix := ""
	if len(preview) > previewLimit {
		preview = preview[:previewLimit]
		suffix = fmt.Sprintf("\n... and %d more", len(origins)-previewLimit)
	}

	content := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("Clear the checked data types for %d origin(s):", len(origins))),
		widget.NewLabel(strings.Join(preview, "\n")+suffix),
		widget.NewSeparator(),
		typeList,
	)

	dialog.NewCustomConfirm("Remove Data", "Remove", "Cancel", content, func(confirmed bool) {
		if !confirmed {
			return
		}

		var selectedTypes []removal.StorageType
		for _, t := range removal.AllStorageTypes {
			if checks[t].Checked {
				selectedTypes = append(selectedTypes, t)
			}
		}
		if len(selectedTypes) == 0 {
			p.status.SetText("No storage types selected — nothing removed.")
			return
		}

		p.removeBtn.Disable()
		p.status.SetText(fmt.Sprintf("Removing data for %d origin(s)...", len(origins)))
		go p.remove(p.client, origins, selectedTypes)
	}, p.win).Show()
}

// removeTimeout scales with batch size since each origin is cleared via
// its own throwaway target/session round trip (see removal.ClearOrigin).
func removeTimeout(n int) time.Duration {
	return scanTimeout + time.Duration(n)*3*time.Second
}

func (p *OriginsPanel) remove(client *cdp.Client, origins []string, types []removal.StorageType) {
	ctx, cancel := context.WithTimeout(context.Background(), removeTimeout(len(origins)))
	defer cancel()

	results := removal.ClearOrigins(ctx, client, origins, types)

	fyne.Do(func() {
		succeeded := 0
		var failed []string
		for _, r := range results {
			if r.Err != nil {
				failed = append(failed, fmt.Sprintf("%s (%v)", r.Origin, r.Err))
				continue
			}
			succeeded++
			delete(p.selected, r.Origin)
		}

		p.updateRemoveButton()
		p.table.Refresh()

		if len(failed) == 0 {
			p.status.SetText(fmt.Sprintf("Cleared data for %d origin(s). Re-scan to confirm.", succeeded))
			return
		}
		p.status.SetText(fmt.Sprintf("Cleared %d of %d origin(s). Failed: %s", succeeded, len(results), strings.Join(failed, "; ")))
	})
}

func (p *OriginsPanel) onScan() {
	client := p.client
	if client == nil {
		return
	}

	p.scanBtn.Disable()
	p.status.SetText("Scanning...")

	go p.scan(client)
}

func (p *OriginsPanel) scan(client *cdp.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()

	origins, err := scan.DiscoverOrigins(ctx, client, 0)

	fyne.Do(func() {
		p.scanBtn.Enable()
		if err != nil {
			p.status.SetText("Scan failed: " + err.Error())
			return
		}
		p.origins = origins
		p.recomputeScores()
		p.status.SetText(fmt.Sprintf("Found %d candidate origin(s).", len(origins)))
	})
}

func (p *OriginsPanel) onCheckPermissions() {
	client := p.client
	if client == nil {
		return
	}

	p.permsBtn.Disable()
	p.status.SetText("Checking permissions (this will briefly switch your active Chrome tab, once per permission type)...")

	go p.checkPermissions(client)
}

func (p *OriginsPanel) checkPermissions(client *cdp.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()

	permissions, err := scan.DiscoverAllPermissions(ctx, client, scan.DefaultPermissionCategories)

	fyne.Do(func() {
		p.permsBtn.Enable()
		if err != nil {
			p.status.SetText("Checking permissions failed: " + err.Error())
			return
		}
		p.permissions = permissions
		p.recomputeScores()

		total := 0
		for _, byCategory := range permissions {
			total += len(byCategory)
		}
		p.status.SetText(fmt.Sprintf("Found %d permission grant(s) across %d origin(s).", total, len(permissions)))
	})
}
