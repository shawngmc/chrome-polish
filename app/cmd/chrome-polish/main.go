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
)

func main() {
	a := app.New()
	w := a.NewWindow("Chrome Polish")

	status := widget.NewLabel("Not connected.")
	connectBtn := widget.NewButton("Connect...", func() {
		status.SetText("TODO: connect to Chrome via CDP")
	})

	w.SetContent(container.NewVBox(
		widget.NewLabel("Chrome Polish"),
		status,
		connectBtn,
	))

	w.Resize(fyne.NewSize(480, 320))
	w.ShowAndRun()
}
