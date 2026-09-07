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
	"github.com/shawngmc/chrome-polish/app/internal/report"
	"github.com/shawngmc/chrome-polish/app/internal/reputation"
	"github.com/shawngmc/chrome-polish/app/internal/scan"
)

const (
	scanTimeout      = 25 * time.Second
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
	colStorage
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
	colStorage:       "Storage",
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
	refreshBtn   *widget.Button
	blocklistBtn *widget.Button
	optionsBtn   *widget.Button
	removeBtn    *widget.Button
	deepCleanBtn *widget.Button
	reportBtn    *widget.Button
	filterEntry  *widget.Entry
	status       *widget.Label
	busy         *widget.ProgressBarInfinite
	detail       *widget.Label
	table        *widget.Table

	origins     []scan.Origin                               // full discovered set, in current sort order
	visible     []scan.Origin                               // origins after the filter box, what the table renders
	filter      string                                      // lowercased, from filterEntry
	permissions map[string]map[string]scan.PermissionStatus // origin -> category -> status
	scores      map[string]reputation.Score                 // origin -> score
	blocklist   reputation.Blocklist
	selected    map[string]bool // origin -> checked, for bulk removal

	// homeGroup and groupNames come from the site-data half of a scan (see
	// scan.DiscoverSiteData / scan.MergeSiteData). homeGroup maps an origin
	// to the GroupingKey of the one SiteGroup it's a non-partitioned member
	// of — the safe direction for a whole-site removal to key off, since a
	// partitioned origin can be a child of many unrelated groups at once.
	homeGroup  map[string]string // origin -> groupingKey
	groupNames map[string]string // groupingKey -> displayName, for dialog text

	// actions is the running log of removal/deep-clean operations
	// performed this session, oldest first — the basis for Save Report.
	// Reset whenever SetClient starts a new session.
	actions []report.Action

	sortCol        int // -1 if unsorted
	sortAsc        bool
	colorBlindMode bool

	// simpleMode is the default UI mode: on connect, origins and
	// permissions are scanned automatically, and the manual Scan/Check
	// buttons are replaced by a single Refresh button. Advanced mode (the
	// prior, fully manual behavior) is reachable from the Options dialog.
	simpleMode bool
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
		simpleMode:  true,
	}

	p.status = widget.NewLabel("Connect first, then scan for candidate origins.")
	p.status.Wrapping = fyne.TextWrapWord

	p.busy = widget.NewProgressBarInfinite()
	p.busy.Hide()

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
	p.table.SetColumnWidth(colStorage, 80)
	p.table.SetColumnWidth(colNotifications, 130)
	p.table.SetColumnWidth(colCamera, 100)
	p.table.SetColumnWidth(colMicrophone, 110)

	p.scanBtn = widget.NewButton("Scan for origins", p.onScan)
	p.scanBtn.Disable()

	p.permsBtn = widget.NewButton(permissionsCheck, p.onCheckPermissions)
	p.permsBtn.Disable()

	p.refreshBtn = widget.NewButton("Refresh", p.onRefresh)
	p.refreshBtn.Disable()

	p.blocklistBtn = widget.NewButton("Load blocklist...", p.onLoadBlocklist)

	p.optionsBtn = widget.NewButton("Options...", p.onOptions)

	p.removeBtn = widget.NewButton("Remove data...", p.onRemove)
	p.removeBtn.Importance = widget.DangerImportance
	p.removeBtn.Disable()

	p.deepCleanBtn = widget.NewButton("Deep clean site(s)...", p.onDeepClean)
	p.deepCleanBtn.Importance = widget.DangerImportance
	p.deepCleanBtn.Disable()

	p.reportBtn = widget.NewButton("Save report...", p.onSaveReport)
	p.reportBtn.Disable()

	p.filterEntry = widget.NewEntry()
	p.filterEntry.SetPlaceHolder("Filter by domain...")
	p.filterEntry.OnChanged = p.onFilterChanged

	p.controls = container.NewVBox(
		p.scanBtn, p.permsBtn, p.refreshBtn, p.blocklistBtn, p.optionsBtn, p.reportBtn,
		widget.NewSeparator(),
		p.busy,
		p.status,
	)
	p.applyMode()

	p.results = container.NewBorder(
		container.NewBorder(nil, nil, widget.NewLabel("Filter:"), nil, p.filterEntry),
		container.NewVBox(p.detail, p.removeBtn, p.deepCleanBtn),
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
	case colStorage:
		return checkmark(hasSource(origin.Sources, scan.SourceStorage))
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
	p.updateDeepCleanButton()
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
	p.updateDeepCleanButton()
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

// selectedGroupingKeys resolves the current selection to the distinct
// GroupingKeys of their non-partitioned home SiteGroups (see homeGroup's
// doc comment), skipping any selected origin with no known home group.
func (p *OriginsPanel) selectedGroupingKeys() []string {
	seen := make(map[string]bool)
	var keys []string
	for origin := range p.selected {
		key, ok := p.homeGroup[origin]
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// updateDeepCleanButton syncs the Deep Clean button's label and enabled
// state to how many distinct site groups the current selection resolves
// to (see selectedGroupingKeys) — not the raw selection count, since
// several selected origins can share one home group, and some may resolve
// to none at all.
func (p *OriginsPanel) updateDeepCleanButton() {
	n := len(p.selectedGroupingKeys())
	if n == 0 {
		p.deepCleanBtn.SetText("Deep clean site(s)...")
		p.deepCleanBtn.Disable()
		return
	}
	p.deepCleanBtn.SetText(fmt.Sprintf("Deep clean site(s) (%d)...", n))
	if p.client != nil {
		p.deepCleanBtn.Enable()
	}
}

// updateReportButton enables Save Report once there's anything worth
// writing down: a completed scan, or an action taken (a report from an
// action-only session, with no scan re-run since, is still meaningful).
func (p *OriginsPanel) updateReportButton() {
	if len(p.origins) > 0 || len(p.actions) > 0 {
		p.reportBtn.Enable()
		return
	}
	p.reportBtn.Disable()
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

// opButtons are the buttons beginOp disables for the duration of an
// operation: at most one of Scan/Check Permissions/Refresh/Remove/Deep Clean
// can run against the shared client at a time.
func (p *OriginsPanel) opButtons() []fyne.Disableable {
	return []fyne.Disableable{p.scanBtn, p.permsBtn, p.refreshBtn, p.removeBtn, p.deepCleanBtn}
}

// beginOp starts an origin-panel operation (scan, permission check, refresh,
// remove, or deep clean): it disables every action button, shows the busy
// indicator, and sets the status text.
func (p *OriginsPanel) beginOp(statusMsg string) {
	for _, b := range p.opButtons() {
		b.Disable()
	}
	p.status.SetText(statusMsg)
	p.busy.Show()
}

// opCurrent reports whether client is still the panel's active connection.
// An operation's goroutine captures the client it was launched with; if
// SetClient has since attached a different one (or none), the goroutine's
// completion is stale and must not touch busy/button state or apply its
// results — a disconnect/reconnect has already reset both.
func (p *OriginsPanel) opCurrent(client *cdp.Client) bool {
	return client == p.client
}

// endOp reverses beginOp once an operation's goroutine completes. Callers on
// a goroutine must wrap this in fyne.Do. If client is no longer the panel's
// active connection, this is a no-op — see opCurrent.
func (p *OriginsPanel) endOp(client *cdp.Client) {
	if !p.opCurrent(client) {
		return
	}
	p.busy.Hide()
	p.scanBtn.Enable()
	p.permsBtn.Enable()
	p.refreshBtn.Enable()
	p.updateRemoveButton()
	p.updateDeepCleanButton()
}

// failDiscovery reports a scan/refresh's origin-discovery failure, unless
// superseded by a disconnect/reconnect since the operation was launched.
func (p *OriginsPanel) failDiscovery(client *cdp.Client, err error) {
	fyne.Do(func() {
		if !p.opCurrent(client) {
			return
		}
		p.status.SetText("Scan failed: " + err.Error())
		p.endOp(client)
	})
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
	p.homeGroup = nil
	p.groupNames = nil
	p.actions = nil
	p.detail.SetText("Select a row to see why it was scored that way.")
	p.filterEntry.SetText("") // triggers onFilterChanged -> applyFilter
	p.updateRemoveButton()
	p.updateDeepCleanButton()
	p.updateReportButton()
	p.table.Refresh()

	if client != nil {
		if p.simpleMode {
			p.beginOp("Connected. Scanning (this will briefly switch your active Chrome tab to read site data)...")
			go p.refresh(client)
		} else {
			p.scanBtn.Enable()
			p.permsBtn.Enable()
			p.refreshBtn.Enable()
			p.status.SetText("Connected. Ready to scan.")
		}
	} else {
		p.busy.Hide()
		p.scanBtn.Disable()
		p.permsBtn.Disable()
		p.refreshBtn.Disable()
		p.status.SetText("Connect first, then scan for candidate origins.")
	}
}

// applyMode shows/hides the manual Scan/Check buttons and the combined
// Refresh button to match simpleMode.
func (p *OriginsPanel) applyMode() {
	if p.simpleMode {
		p.scanBtn.Hide()
		p.permsBtn.Hide()
		p.refreshBtn.Show()
	} else {
		p.scanBtn.Show()
		p.permsBtn.Show()
		p.refreshBtn.Hide()
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
	case colStorage:
		return boolKey(hasSource(o.Sources, scan.SourceStorage))
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
	p.updateReportButton()
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
	colorBlindCheck := widget.NewCheck("Color-blind friendly score colors", func(checked bool) {
		p.colorBlindMode = checked
		p.table.Refresh()
	})
	colorBlindCheck.SetChecked(p.colorBlindMode)

	advancedCheck := widget.NewCheck("Advanced mode (manual scan and permission checks)", func(checked bool) {
		p.simpleMode = !checked
		p.applyMode()
	})
	advancedCheck.SetChecked(!p.simpleMode)

	content := container.NewVBox(colorBlindCheck, advancedCheck)
	dialog.ShowCustom("Options", "Close", content, p.win)
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

// onSaveReport writes a plain-text summary of the session (scored origins
// plus every removal/deep-clean action taken) to a file the person picks —
// meant to be left with them as a record, per DESIGN.md's one-on-one
// tech-support framing. Pure local file I/O over already-collected state;
// it doesn't touch the CDP client, so it's available even mid-operation.
func (p *OriginsPanel) onSaveReport() {
	scores := make([]reputation.Score, 0, len(p.scores))
	for _, s := range p.scores {
		scores = append(scores, s)
	}

	text := report.Generate(report.Input{
		GeneratedAt: time.Now(),
		Scores:      scores,
		Actions:     p.actions,
	})

	save := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, p.win)
			return
		}
		if writer == nil {
			return // cancelled
		}
		defer writer.Close()

		if _, err := writer.Write([]byte(text)); err != nil {
			dialog.ShowError(err, p.win)
			return
		}
		p.status.SetText("Saved session report to " + writer.URI().Name() + ".")
	}, p.win)
	save.SetFileName(fmt.Sprintf("chrome-polish-report-%s.txt", time.Now().Format("20060102-150405")))
	save.Show()
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

		p.beginOp(fmt.Sprintf("Removing data for %d origin(s)...", len(origins)))
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

	typeNames := make([]string, len(types))
	for i, t := range types {
		typeNames[i] = string(t)
	}
	now := time.Now()

	fyne.Do(func() {
		if !p.opCurrent(client) {
			return
		}
		defer p.endOp(client)

		succeeded := 0
		var failed []string
		for _, r := range results {
			p.actions = append(p.actions, report.Action{
				Time: now, Kind: "Removed data", Target: r.Origin, Types: typeNames, Err: r.Err,
			})
			if r.Err != nil {
				failed = append(failed, fmt.Sprintf("%s (%v)", r.Origin, r.Err))
				continue
			}
			succeeded++
			delete(p.selected, r.Origin)
		}

		p.table.Refresh()
		p.updateReportButton()

		if len(failed) == 0 {
			p.status.SetText(fmt.Sprintf("Cleared data for %d origin(s). Re-scan to confirm.", succeeded))
			return
		}
		p.status.SetText(fmt.Sprintf("Cleared %d of %d origin(s). Failed: %s", succeeded, len(results), strings.Join(failed, "; ")))
	})
}

// onDeepClean opens a confirmation dialog for wholly clearing the site
// group(s) the current selection resolves to (see selectedGroupingKeys) —
// every origin under each group, partitioned or not, all storage types at
// once. This is the only way to reach storage-partitioned data at all
// (removal.ClearSiteGroup's docs explain why Storage.clearDataForOrigin
// can't), but it's a bigger blast radius than "Remove data": it clears the
// whole site, not just the checked origin(s), and offers no per-type
// selection.
func (p *OriginsPanel) onDeepClean() {
	if len(p.selected) == 0 || p.client == nil {
		return
	}

	keys := p.selectedGroupingKeys()
	if len(keys) == 0 {
		p.status.SetText("None of the selected origin(s) have a known site group to deep clean — re-scan first?")
		return
	}

	names := make([]string, 0, len(keys))
	for _, k := range keys {
		if name, ok := p.groupNames[k]; ok {
			names = append(names, name)
		} else {
			names = append(names, k)
		}
	}
	sort.Strings(names)

	skipped := 0
	for origin := range p.selected {
		if _, ok := p.homeGroup[origin]; !ok {
			skipped++
		}
	}

	lines := []fyne.CanvasObject{
		widget.NewLabel(fmt.Sprintf("This clears EVERYTHING for %d whole site(s) — cookies, local storage, cache, and any storage partitioned under them (e.g. third-party embeds) — not just the checked origin(s):", len(keys))),
		widget.NewLabel(strings.Join(names, "\n")),
	}
	if skipped > 0 {
		lines = append(lines, widget.NewLabel(fmt.Sprintf("%d selected origin(s) have no known site group and will be skipped.", skipped)))
	}
	content := container.NewVBox(lines...)

	dialog.NewCustomConfirm("Deep Clean Site(s)", "Clear Everything", "Cancel", content, func(confirmed bool) {
		if !confirmed {
			return
		}

		p.beginOp(fmt.Sprintf("Deep cleaning %d site(s)...", len(keys)))
		go p.deepClean(p.client, keys)
	}, p.win).Show()
}

func (p *OriginsPanel) deepClean(client *cdp.Client, groupingKeys []string) {
	ctx, cancel := context.WithTimeout(context.Background(), removeTimeout(len(groupingKeys)))
	defer cancel()

	results := removal.ClearSiteGroups(ctx, client, groupingKeys)
	now := time.Now()

	fyne.Do(func() {
		if !p.opCurrent(client) {
			return
		}
		defer p.endOp(client)

		succeededKeys := make(map[string]bool, len(results))
		succeeded := 0
		var failed []string
		for _, r := range results {
			name := r.GroupingKey
			if n, ok := p.groupNames[r.GroupingKey]; ok {
				name = n
			}
			p.actions = append(p.actions, report.Action{
				Time: now, Kind: "Deep cleaned site", Target: name, Err: r.Err,
			})
			if r.Err != nil {
				failed = append(failed, fmt.Sprintf("%s (%v)", r.GroupingKey, r.Err))
				continue
			}
			succeeded++
			succeededKeys[r.GroupingKey] = true
		}

		for origin := range p.selected {
			if key, ok := p.homeGroup[origin]; ok && succeededKeys[key] {
				delete(p.selected, origin)
			}
		}

		p.table.Refresh()
		p.updateReportButton()

		if len(failed) == 0 {
			p.status.SetText(fmt.Sprintf("Deep cleaned %d site(s). Re-scan to confirm.", succeeded))
			return
		}
		p.status.SetText(fmt.Sprintf("Deep cleaned %d of %d site(s). Failed: %s", succeeded, len(results), strings.Join(failed, "; ")))
	})
}

func (p *OriginsPanel) onScan() {
	client := p.client
	if client == nil {
		return
	}

	p.beginOp("Scanning (this will briefly switch your active Chrome tab to read site data)...")
	go p.scan(client)
}

// originScan holds the result of discoverOrigins, so callers that need to
// combine it with other work (see refresh) can hold onto it before touching
// panel state on the Fyne main thread.
type originScan struct {
	merged      []scan.Origin
	homeGroup   map[string]string
	groupNames  map[string]string
	numGroups   int
	siteDataErr error
}

// discoverOrigins runs origin discovery and site-data grouping against
// client, merging their results the same way for every caller (onScan and
// the combined refresh flow alike).
func discoverOrigins(ctx context.Context, client *cdp.Client) (originScan, error) {
	origins, err := scan.DiscoverOrigins(ctx, client, 0)
	if err != nil {
		return originScan{}, err
	}

	// DiscoverSiteData surfaces origins with storage but no cookie and no
	// service worker at all (see scan.SourceStorage's doc comment) — a
	// failure here shouldn't discard what DiscoverOrigins already found.
	groups, siteDataErr := scan.DiscoverSiteData(ctx, client)

	res := originScan{merged: origins, siteDataErr: siteDataErr, numGroups: len(groups)}
	if siteDataErr == nil {
		res.merged, res.homeGroup = scan.MergeSiteData(origins, groups)

		res.groupNames = make(map[string]string, len(groups))
		for _, g := range groups {
			res.groupNames[g.GroupingKey] = g.DisplayName
		}
	}
	return res, nil
}

func (p *OriginsPanel) scan(client *cdp.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()

	res, err := discoverOrigins(ctx, client)
	if err != nil {
		p.failDiscovery(client, err)
		return
	}

	fyne.Do(func() {
		if !p.opCurrent(client) {
			return
		}
		p.origins = res.merged
		p.homeGroup = res.homeGroup
		p.groupNames = res.groupNames
		p.recomputeScores()
		defer p.endOp(client)

		if res.siteDataErr != nil {
			p.status.SetText(fmt.Sprintf("Found %d candidate origin(s). Site-data scan failed: %v", len(res.merged), res.siteDataErr))
			return
		}
		p.status.SetText(fmt.Sprintf("Found %d candidate origin(s) (%d site group(s) scanned).", len(res.merged), res.numGroups))
	})
}

func (p *OriginsPanel) onCheckPermissions() {
	client := p.client
	if client == nil {
		return
	}

	p.beginOp("Checking permissions (this will briefly switch your active Chrome tab, once per permission type)...")
	go p.checkPermissions(client)
}

func (p *OriginsPanel) checkPermissions(client *cdp.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()

	permissions, err := scan.DiscoverAllPermissions(ctx, client, scan.DefaultPermissionCategories)

	fyne.Do(func() {
		if !p.opCurrent(client) {
			return
		}
		defer p.endOp(client)
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

// onRefresh is simple mode's single entry point, replacing the manual
// Scan/Check buttons: it runs origin discovery and a permissions check back
// to back and reports one combined status line, so someone in simple mode
// never has to know these are two separate CDP round trips.
func (p *OriginsPanel) onRefresh() {
	client := p.client
	if client == nil {
		return
	}

	p.beginOp("Scanning (this will briefly switch your active Chrome tab to read site data)...")
	go p.refresh(client)
}

func (p *OriginsPanel) refresh(client *cdp.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()

	res, err := discoverOrigins(ctx, client)
	if err != nil {
		p.failDiscovery(client, err)
		return
	}

	fyne.Do(func() {
		if p.opCurrent(client) {
			p.status.SetText("Checking permissions (this will briefly switch your active Chrome tab, once per permission type)...")
		}
	})

	permCtx, permCancel := context.WithTimeout(context.Background(), scanTimeout)
	defer permCancel()
	permissions, permErr := scan.DiscoverAllPermissions(permCtx, client, scan.DefaultPermissionCategories)

	fyne.Do(func() {
		if !p.opCurrent(client) {
			return
		}
		p.origins = res.merged
		p.homeGroup = res.homeGroup
		p.groupNames = res.groupNames
		if permErr == nil {
			p.permissions = permissions
		}
		p.recomputeScores()

		msg := fmt.Sprintf("Found %d candidate origin(s)", len(res.merged))
		if res.siteDataErr != nil {
			msg += fmt.Sprintf(" (site-data scan failed: %v)", res.siteDataErr)
		} else {
			msg += fmt.Sprintf(" (%d site group(s) scanned)", res.numGroups)
		}
		if permErr != nil {
			msg += fmt.Sprintf(". Checking permissions failed: %v", permErr)
		} else {
			total := 0
			for _, byCategory := range permissions {
				total += len(byCategory)
			}
			msg += fmt.Sprintf(". Found %d permission grant(s) across %d origin(s).", total, len(permissions))
		}
		p.status.SetText(msg)
		p.endOp(client)
	})
}
