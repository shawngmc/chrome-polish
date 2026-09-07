package report

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/reputation"
)

func TestGenerateFlaggedOriginsSortedHighestFirst(t *testing.T) {
	in := Input{
		GeneratedAt: time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
		Scores: []reputation.Score{
			{Origin: "https://low.example", Total: 20, Signals: []reputation.Signal{
				{Name: "abused-tld", Weight: 20, Detail: "top-level domain has a history of abuse"},
			}},
			{Origin: "https://clean.example", Total: 0},
			{Origin: "https://high.example", Total: 100, Signals: []reputation.Signal{
				{Name: "blocklist", Weight: 100, Detail: "hostname is on the user-supplied blocklist"},
			}},
		},
	}

	out := Generate(in)

	high := strings.Index(out, "high.example")
	low := strings.Index(out, "low.example")
	if high == -1 || low == -1 || high > low {
		t.Fatalf("expected high.example before low.example in report, got:\n%s", out)
	}
	if strings.Contains(out, "clean.example") {
		t.Errorf("clean origin (score 0) should not be listed individually, got:\n%s", out)
	}
	if !strings.Contains(out, "3 origin(s) scanned: 2 flagged, 1 clean") {
		t.Errorf("expected summary counts in report, got:\n%s", out)
	}
}

func TestGenerateNoFlaggedOrigins(t *testing.T) {
	out := Generate(Input{Scores: []reputation.Score{{Origin: "https://example.com", Total: 0}}})

	if !strings.Contains(out, "None. Every scanned origin came back clean.") {
		t.Errorf("expected all-clean note, got:\n%s", out)
	}
}

func TestGenerateActionsLoggedWithOutcome(t *testing.T) {
	in := Input{
		Actions: []Action{
			{
				Time:   time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
				Kind:   "Removed data",
				Target: "https://bad.example",
				Types:  []string{"cookies", "service_workers"},
			},
			{
				Time:   time.Date(2026, 1, 2, 15, 5, 0, 0, time.UTC),
				Kind:   "Deep cleaned site",
				Target: "worse.example",
				Err:    errors.New("connection reset"),
			},
		},
	}

	out := Generate(in)

	if !strings.Contains(out, "Removed data: https://bad.example (cookies, service_workers)") {
		t.Errorf("expected succeeded action line, got:\n%s", out)
	}
	if !strings.Contains(out, "Deep cleaned site: worse.example — FAILED: connection reset") {
		t.Errorf("expected failed action line, got:\n%s", out)
	}
	if !strings.Contains(out, "2 action(s) taken: 1 succeeded, 1 failed") {
		t.Errorf("expected action summary counts, got:\n%s", out)
	}
}

func TestGenerateNoActions(t *testing.T) {
	out := Generate(Input{})

	if !strings.Contains(out, "None. No data was removed this session.") {
		t.Errorf("expected no-actions note, got:\n%s", out)
	}
}
