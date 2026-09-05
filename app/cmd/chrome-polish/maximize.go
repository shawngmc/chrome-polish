package main

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
)

// titleBarAllowance is subtracted from the computed window height so the
// window (content + OS-drawn title bar) fits within the screen instead of
// running slightly past its bottom edge.
const titleBarAllowance = 40

// maximizeWindow sizes w to fill the primary screen's usable area — the
// closest equivalent to a native "maximize", since Fyne has no maximize
// API of its own (only true fullscreen, which drops window chrome
// entirely). Falls back to a generously large fixed size where the
// screen-size lookup isn't implemented.
func maximizeWindow(w fyne.Window) {
	if size, ok := screenSize(); ok {
		w.Resize(fyne.NewSize(size.Width, size.Height-titleBarAllowance))
		w.CenterOnScreen()
		return
	}
	w.Resize(fyne.NewSize(1440, 900))
	w.CenterOnScreen()
}

func screenSize() (fyne.Size, bool) {
	if runtime.GOOS != "darwin" {
		return fyne.Size{}, false
	}

	// "bounds of window of desktop" reports the visible desktop area
	// (menu bar and dock already excluded) as "x1, y1, x2, y2", in
	// points — the same logical-pixel unit Fyne sizes use, so this needs
	// no extra HiDPI scaling.
	out, err := exec.Command("osascript", "-e", `tell application "Finder" to get bounds of window of desktop`).Output()
	if err != nil {
		return fyne.Size{}, false
	}

	parts := strings.Split(strings.TrimSpace(string(out)), ", ")
	if len(parts) != 4 {
		return fyne.Size{}, false
	}
	coords := make([]int, 4)
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			return fyne.Size{}, false
		}
		coords[i] = v
	}

	width := coords[2] - coords[0]
	height := coords[3] - coords[1]
	if width <= 0 || height <= 0 {
		return fyne.Size{}, false
	}

	return fyne.NewSize(float32(width), float32(height)), true
}
