package ui

import "image/color"

// colorStop is one point on a score-to-color gradient.
type colorStop struct {
	score int
	color color.RGBA
}

// scoreColorStops is the default green -> yellow -> orange -> red scale.
var scoreColorStops = []colorStop{
	{0, color.RGBA{0x4c, 0xaf, 0x50, 0xff}},   // green
	{20, color.RGBA{0xfb, 0xc0, 0x2d, 0xff}},  // yellow
	{50, color.RGBA{0xf5, 0x7c, 0x00, 0xff}},  // orange
	{100, color.RGBA{0xe5, 0x39, 0x35, 0xff}}, // red
}

// scoreColorStopsColorBlind swaps the green/red endpoints for blue/purple —
// the two colors people with red-green color vision deficiency (the most
// common forms) can reliably tell apart from the shared yellow/orange
// midpoints and from each other. Green and red never appear together.
var scoreColorStopsColorBlind = []colorStop{
	{0, color.RGBA{0x1e, 0x88, 0xe5, 0xff}},   // blue
	{20, color.RGBA{0xfb, 0xc0, 0x2d, 0xff}},  // yellow (shared)
	{50, color.RGBA{0xf5, 0x7c, 0x00, 0xff}},  // orange (shared)
	{100, color.RGBA{0x8e, 0x24, 0xaa, 0xff}}, // purple
}

// scoreColor interpolates a color for score along stops, clamping to the
// first/last stop's color outside their range.
func scoreColor(stops []colorStop, score int) color.Color {
	if score <= stops[0].score {
		return stops[0].color
	}
	last := len(stops) - 1
	if score >= stops[last].score {
		return stops[last].color
	}
	for i := 0; i < last; i++ {
		a, b := stops[i], stops[i+1]
		if score >= a.score && score <= b.score {
			t := float64(score-a.score) / float64(b.score-a.score)
			return lerpRGBA(a.color, b.color, t)
		}
	}
	return stops[last].color
}

func lerpRGBA(a, b color.RGBA, t float64) color.Color {
	lerp := func(x, y uint8) uint8 {
		return uint8(float64(x) + t*(float64(y)-float64(x)))
	}
	return color.RGBA{
		R: lerp(a.R, b.R),
		G: lerp(a.G, b.G),
		B: lerp(a.B, b.B),
		A: 0xff,
	}
}
