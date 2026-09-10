// Command chrome-polish is the control UI for connecting to a Chrome/Chromium
// instance over the Chrome DevTools Protocol and cleaning up
// scareware/malvertising artifacts from a browser profile.
//
// See DESIGN.md at the repo root for the full architecture.
package main

import (
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/shawngmc/chrome-polish/app/internal/blocklist"
	"github.com/shawngmc/chrome-polish/app/internal/cdp"
	"github.com/shawngmc/chrome-polish/app/internal/ui"
)

func main() {
	a := app.NewWithID("com.github.shawngmc.chrome-polish")
	a.SetIcon(resourceIconPng)
	w := a.NewWindow("Chrome Polish")

	title := ui.Wordmark()

	connectPanel := ui.NewConnectPanel()
	originsPanel := ui.NewOriginsPanel(w)

	connectPanel.OnConnected = func(client *cdp.Client) {
		originsPanel.SetClient(client)
	}
	connectPanel.OnDisconnected = func() {
		originsPanel.SetClient(nil)
	}

	blocklistMgr := blocklist.NewManager(a.Storage().RootURI().Path())
	_ = blocklistMgr.Load() // degrades to an empty set on a missing/corrupt index

	blocklistsPanel := ui.NewBlocklistsPanel(w, blocklistMgr)
	blocklistsPanel.OnBlocklistChanged = originsPanel.SetBlocklist
	originsPanel.SetBlocklist(blocklistMgr.Compiled())

	sidebar := container.NewVBox(
		title,
		widget.NewSeparator(),
		connectPanel.Container(),
		widget.NewSeparator(),
		originsPanel.Controls(),
		widget.NewSeparator(),
		blocklistsPanel.Container(),
	)

	w.SetContent(container.NewBorder(
		nil, nil,
		sidebar, nil,
		originsPanel.Results(),
	))

	if blocklist.ShouldPromptForDefaults(blocklistMgr, a.Preferences()) {
		ui.ShowOisdPrompt(w, blocklistMgr, a.Preferences(), func() {
			originsPanel.SetBlocklist(blocklistMgr.Compiled())
			blocklistsPanel.RefreshSummary()
		})
	}

	maximizeWindow(w)
	w.ShowAndRun()
}
