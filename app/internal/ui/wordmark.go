package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

//go:embed assets/wordmark.png
var wordmarkPNG []byte

// wordmarkAspect is the wordmark image's width÷height (638×184 — the
// pill graphic cropped tight to its visible content, not the original
// 800×280 canvas it was rendered on, which had ~87px of transparent
// clear-space margin baked in on every side).
const wordmarkAspect = 638.0 / 184.0

// wordmarkMargin is the fixed gap kept on each side of the wordmark
// within its column.
const wordmarkMargin = 6

// wordmarkReservedWidth is the widest the sidebar column is expected to
// get (observed ~225pt in the running app — Fyne sizes are in points,
// not the device pixels a Retina screenshot reports). Fyne's VBox locks
// a child's row height to whatever MinSize reports before the row's
// final width is known (layout/boxlayout.go), so MinSize has to reserve
// height up front; this bounds that guess. Too generous a guess here
// is exactly what left a large dead gap below the wordmark before.
const wordmarkReservedWidth = 240

// Wordmark returns the "Chrome Polish" wordmark image, sized for use as
// the app's title in place of a plain text heading. It fills the width
// of whatever column it's placed in — e.g. the control sidebar — minus
// wordmarkMargin on each side, while keeping its aspect ratio.
func Wordmark() fyne.CanvasObject {
	res := fyne.NewStaticResource("wordmark.png", wordmarkPNG)
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	return container.New(wordmarkLayout{}, img)
}

type wordmarkLayout struct{}

func (wordmarkLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	w := size.Width - wordmarkMargin*2
	if w < 0 {
		w = 0
	}
	objects[0].Resize(fyne.NewSize(w, w/wordmarkAspect))
	objects[0].Move(fyne.NewPos(wordmarkMargin, 0))
}

func (wordmarkLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	w := float32(wordmarkReservedWidth - wordmarkMargin*2)
	return fyne.NewSize(0, w/wordmarkAspect)
}
