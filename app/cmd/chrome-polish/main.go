// Command chrome-polish is the control UI for connecting to a Chrome/Chromium
// instance over the Chrome DevTools Protocol and cleaning up
// scareware/malvertising artifacts from a browser profile.
//
// See DESIGN.md at the repo root for the full architecture.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
	"github.com/shawngmc/chrome-polish/app/internal/ui"
)

func main() {
	a := app.New()
	w := a.NewWindow("Chrome Polish")

	connectPanel := ui.NewConnectPanel()
	originsPanel := ui.NewOriginsPanel(w)

	connectPanel.OnConnected = func(client *cdp.Client) {
		originsPanel.SetClient(client)
	}
	connectPanel.OnDisconnected = func() {
		originsPanel.SetClient(nil)
	}

	w.SetContent(container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Chrome Polish"),
			connectPanel.Container(),
			widget.NewSeparator(),
		),
		nil, nil, nil,
		originsPanel.Container(),
	))

	w.Resize(fyne.NewSize(560, 480))
	w.ShowAndRun()
}
