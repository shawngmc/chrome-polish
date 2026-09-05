package ui

import (
	"image/color"
	"testing"
)

func TestScoreColorStops(t *testing.T) {
	tests := []struct {
		score int
		want  color.RGBA
	}{
		{0, scoreColorStops[0].color},
		{20, scoreColorStops[1].color},
		{50, scoreColorStops[2].color},
		{100, scoreColorStops[3].color},
		{-10, scoreColorStops[0].color},  // clamps below range
		{1000, scoreColorStops[3].color}, // clamps above range
	}

	for _, tt := range tests {
		got := scoreColor(scoreColorStops, tt.score)
		if got != tt.want {
			t.Errorf("scoreColor(%d) = %+v, want %+v", tt.score, got, tt.want)
		}
	}
}

func TestScoreColorInterpolatesBetweenStops(t *testing.T) {
	// Halfway between the 0 (green) and 20 (yellow) stops.
	got := scoreColor(scoreColorStops, 10).(color.RGBA)
	green := scoreColorStops[0].color
	yellow := scoreColorStops[1].color

	inRange := func(v, a, b uint8) bool {
		if a > b {
			a, b = b, a
		}
		return a <= v && v <= b
	}
	if !inRange(got.R, green.R, yellow.R) || !inRange(got.G, green.G, yellow.G) || !inRange(got.B, green.B, yellow.B) {
		t.Errorf("scoreColor(10) = %+v, want each channel between green %+v and yellow %+v", got, green, yellow)
	}
	if got == green || got == yellow {
		t.Errorf("scoreColor(10) = %+v, expected a blend, not an endpoint", got)
	}
}

func TestColorBlindPaletteAvoidsRedGreen(t *testing.T) {
	for _, stop := range scoreColorStopsColorBlind {
		if stop.color == scoreColorStops[0].color || stop.color == scoreColorStops[3].color {
			t.Errorf("color-blind palette stop %+v reuses a green/red color from the default palette", stop)
		}
	}
}
