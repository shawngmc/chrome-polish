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

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
	"github.com/shawngmc/chrome-polish/app/internal/ui"
)

func main() {
	a := app.New()
	w := a.NewWindow("Chrome Polish")

	title := widget.NewRichText(&widget.TextSegment{
		Text:  "Chrome Polish",
		Style: widget.RichTextStyleHeading,
	})

	connectPanel := ui.NewConnectPanel()
	originsPanel := ui.NewOriginsPanel(w)

	connectPanel.OnConnected = func(client *cdp.Client) {
		originsPanel.SetClient(client)
	}
	connectPanel.OnDisconnected = func() {
		originsPanel.SetClient(nil)
	}

	sidebar := container.NewVBox(
		title,
		widget.NewSeparator(),
		connectPanel.Container(),
		widget.NewSeparator(),
		originsPanel.Controls(),
	)

	w.SetContent(container.NewBorder(
		nil, nil,
		sidebar, nil,
		originsPanel.Results(),
	))

	maximizeWindow(w)
	w.ShowAndRun()
}
