package ui

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/shawngmc/chrome-polish/app/internal/blocklist"
	"github.com/shawngmc/chrome-polish/app/internal/reputation"
)

// blocklistOpTimeout bounds a single add/refresh network fetch triggered
// from this panel. blocklist.Manager's own default fetch already enforces
// a per-request timeout; this is the context passed in, so it just needs
// to be at least that long.
const blocklistOpTimeout = 30 * time.Second

// BlocklistsPanel is the sidebar entry for managing blocklist sources
// (see internal/blocklist): a compact summary plus a button that opens a
// modal with the full add/remove/refresh/enable table, following
// ConnectPanel's constructor/Container() shape.
type BlocklistsPanel struct {
	win fyne.Window
	mgr *blocklist.Manager

	summary   *widget.Label
	manageBtn *widget.Button
	root      fyne.CanvasObject

	// OnBlocklistChanged is called on the Fyne main thread every time the
	// compiled blocklist changes (add, remove, enable/disable, refresh).
	OnBlocklistChanged func(reputation.Blocklist)
}

// NewBlocklistsPanel builds a ready-to-use panel over an already-loaded
// Manager.
func NewBlocklistsPanel(win fyne.Window, mgr *blocklist.Manager) *BlocklistsPanel {
	p := &BlocklistsPanel{win: win, mgr: mgr}

	p.summary = widget.NewLabel("")
	p.summary.Wrapping = fyne.TextWrapWord
	p.manageBtn = widget.NewButton("Manage Blocklists...", p.onManage)

	p.root = container.NewVBox(p.summary, p.manageBtn)
	p.RefreshSummary()

	return p
}

// Container returns the panel's sidebar summary + button.
func (p *BlocklistsPanel) Container() fyne.CanvasObject {
	return p.root
}

// RefreshSummary re-renders the sidebar summary from the current state of
// the underlying Manager. Exported so a caller that mutates the Manager
// directly rather than through this panel (namely main.go's startup oisd
// prompt, via ShowOisdPrompt) can resync it afterward.
func (p *BlocklistsPanel) RefreshSummary() {
	sources := p.mgr.Sources()
	enabled := 0
	for _, s := range sources {
		if s.Enabled {
			enabled++
		}
	}
	n := len(p.mgr.Compiled())
	p.summary.SetText(fmt.Sprintf(
		"%d %s (%d active), %d %s",
		len(sources), pluralize(len(sources), "blocklist", "blocklists"),
		enabled,
		n, pluralize(n, "entry", "entries"),
	))
}

// notifyChanged refreshes the sidebar summary and, if set, tells the
// caller (main.go, wiring to OriginsPanel.SetBlocklist) about the new
// compiled blocklist. Called after every mutation.
func (p *BlocklistsPanel) notifyChanged() {
	p.RefreshSummary()
	if p.OnBlocklistChanged != nil {
		p.OnBlocklistChanged(p.mgr.Compiled())
	}
}

// Blocklist manager table columns.
const (
	blColEnabled = iota
	blColName
	blColKind
	blColLocation
	blColLastFetched
	blColEntries
	blColStatus
	blNumCols
)

var blColumnTitles = [blNumCols]string{"On", "Name", "Kind", "Location", "Last Fetched", "Entries", "Status"}
var blColumnWidths = [blNumCols]float32{36, 140, 50, 220, 150, 70, 220}

// onManage opens the modal blocklist manager, mirroring OriginsPanel's
// onOptions dialog.ShowCustom pattern. Table/selection/status state is
// local to this call (rebuilt fresh each time the dialog opens), since
// the manager itself (p.mgr) is the durable source of truth.
func (p *BlocklistsPanel) onManage() {
	var rows []blocklist.Source
	selected := -1

	var table *widget.Table
	table = widget.NewTable(
		func() (int, int) { return len(rows) + 1, blNumCols },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			label := obj.(*widget.Label)
			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText(blColumnTitles[id.Col])
				return
			}
			label.TextStyle = fyne.TextStyle{}
			src := rows[id.Row-1]
			switch id.Col {
			case blColEnabled:
				label.SetText(checkmark(src.Enabled))
			case blColName:
				label.SetText(src.Name)
			case blColKind:
				label.SetText(string(src.Kind))
			case blColLocation:
				label.SetText(src.Location)
			case blColLastFetched:
				if src.LastFetched.IsZero() {
					label.SetText("never")
				} else {
					label.SetText(src.LastFetched.Format("Jan 2, 2006 3:04 PM"))
				}
			case blColEntries:
				label.SetText(strconv.Itoa(src.EntryCount))
			case blColStatus:
				label.SetText(src.LastError)
			}
		},
	)
	for i, w := range blColumnWidths {
		table.SetColumnWidth(i, w)
	}

	status := widget.NewLabel("Select a row, then Refresh, Enable/Disable, or Remove it.")
	status.Wrapping = fyne.TextWrapWord

	reload := func() {
		rows = p.mgr.Sources()
		if selected >= len(rows) {
			selected = -1
		}
		table.Refresh()
	}
	reload()

	table.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 {
			table.UnselectAll()
			selected = -1
			return
		}
		selected = id.Row - 1
	}

	withSelected := func(action func(src blocklist.Source)) func() {
		return func() {
			if selected < 0 || selected >= len(rows) {
				status.SetText("Select a blocklist first.")
				return
			}
			action(rows[selected])
		}
	}

	toggleBtn := widget.NewButton("Enable/Disable", withSelected(func(src blocklist.Source) {
		if err := p.mgr.SetEnabled(src.ID, !src.Enabled); err != nil {
			dialog.ShowError(err, p.win)
			return
		}
		reload()
		p.notifyChanged()
	}))

	refreshBtn := widget.NewButton("Refresh", withSelected(func(src blocklist.Source) {
		status.SetText("Refreshing " + src.Name + "...")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), blocklistOpTimeout)
			defer cancel()
			err := p.mgr.Refresh(ctx, src.ID)
			fyne.Do(func() {
				if err != nil {
					status.SetText("Refresh of " + src.Name + " failed: " + err.Error())
				} else {
					status.SetText("Refreshed " + src.Name + ".")
				}
				reload()
				p.notifyChanged()
			})
		}()
	}))

	removeBtn := widget.NewButton("Remove", withSelected(func(src blocklist.Source) {
		dialog.ShowConfirm("Remove blocklist",
			fmt.Sprintf("Remove %q? This deletes its cached content.", src.Name),
			func(ok bool) {
				if !ok {
					return
				}
				if err := p.mgr.Remove(src.ID); err != nil {
					dialog.ShowError(err, p.win)
					return
				}
				selected = -1
				reload()
				p.notifyChanged()
			}, p.win)
	}))
	removeBtn.Importance = widget.DangerImportance

	addURLBtn := widget.NewButton("Add via URL...", func() { p.onAddURL(status, reload) })
	addFileBtn := widget.NewButton("Add via file...", func() { p.onAddFile(status, reload) })

	content := container.NewBorder(
		nil,
		container.NewVBox(
			container.NewGridWithColumns(2, addURLBtn, addFileBtn),
			container.NewGridWithColumns(3, toggleBtn, refreshBtn, removeBtn),
			status,
		),
		nil, nil,
		table,
	)

	d := dialog.NewCustom("Manage Blocklists", "Close", content, p.win)
	d.Resize(fyne.NewSize(760, 420))
	d.Show()
}

// onAddURL prompts for a name + URL, then fetches it in the background.
func (p *BlocklistsPanel) onAddURL(status *widget.Label, reload func()) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Display name (optional)")
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("https://example.com/list.txt")

	form := widget.NewForm(
		widget.NewFormItem("Name", nameEntry),
		widget.NewFormItem("URL", urlEntry),
	)

	dialog.ShowCustomConfirm("Add blocklist from URL", "Add", "Cancel", form, func(ok bool) {
		if !ok || urlEntry.Text == "" {
			return
		}
		rawURL := urlEntry.Text
		name := nameEntry.Text

		status.SetText("Fetching " + rawURL + "...")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), blocklistOpTimeout)
			defer cancel()
			src, err := p.mgr.AddURL(ctx, rawURL, name)
			fyne.Do(func() {
				if err != nil {
					status.SetText("")
					dialog.ShowError(err, p.win)
					return
				}
				status.SetText(fmt.Sprintf("Added %s (%d entries).", src.Name, src.EntryCount))
				reload()
				p.notifyChanged()
			})
		}()
	}, p.win)
}

// onAddFile prompts for a local file via the native file picker (same
// dialog.ShowFileOpen shape the old single-list flow used), reading it
// synchronously — local disk I/O, not worth a goroutine hop for the sizes
// a person picks by hand.
func (p *BlocklistsPanel) onAddFile(status *widget.Label, reload func()) {
	dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, p.win)
			return
		}
		if reader == nil {
			return // cancelled
		}
		path := reader.URI().Path()
		name := reader.URI().Name()
		reader.Close()

		src, err := p.mgr.AddFile(name, path)
		if err != nil {
			dialog.ShowError(err, p.win)
			return
		}
		status.SetText(fmt.Sprintf("Added %s (%d entries).", src.Name, src.EntryCount))
		reload()
		p.notifyChanged()
	}, p.win)
}

// ShowOisdPrompt offers to download oisd.nl's "big" and "nsfw" blocklists
// (see internal/blocklist/oisd.go) — shown once at startup when neither is
// cached yet (blocklist.ShouldPromptForDefaults). They're GPLv3-licensed,
// so this app fetches and caches them at runtime instead of bundling
// them. onDownloaded is called on the Fyne main thread after a successful
// (or partially successful) download, so the caller can refresh dependent
// UI (the origins table's blocklist, this panel's summary).
func ShowOisdPrompt(win fyne.Window, mgr *blocklist.Manager, prefs fyne.Preferences, onDownloaded func()) {
	msg := widget.NewLabel(
		"chrome-polish can use the oisd.nl \"big\" and \"nsfw\" blocklists to flag " +
			"known bad and adult origins during a scan.\n\n" +
			"These lists are GPLv3-licensed and aren't bundled with the app — " +
			"they'll be downloaded now and cached in your profile.",
	)
	msg.Wrapping = fyne.TextWrapWord

	dontAsk := widget.NewCheck("Don't ask again", nil)

	content := container.NewVBox(msg, dontAsk)

	dialog.ShowCustomConfirm("Download recommended blocklists?", "Download", "Not now", content, func(ok bool) {
		if !ok {
			if dontAsk.Checked {
				prefs.SetBool(blocklist.PrefSkipOisdPrompt, true)
			}
			return
		}

		go func() {
			ctx := context.Background()
			_, errBig := mgr.AddURL(ctx, blocklist.OisdBigURL, blocklist.OisdBigName)
			_, errNSFW := mgr.AddURL(ctx, blocklist.OisdNSFWURL, blocklist.OisdNSFWName)

			fyne.Do(func() {
				switch {
				case errBig != nil && errNSFW != nil:
					dialog.ShowError(fmt.Errorf("oisd big: %w\noisd nsfw: %w", errBig, errNSFW), win)
				case errBig != nil:
					dialog.ShowError(fmt.Errorf("oisd big: %w", errBig), win)
				case errNSFW != nil:
					dialog.ShowError(fmt.Errorf("oisd nsfw: %w", errNSFW), win)
				}
				onDownloaded()
			})
		}()
	}, win)
}
