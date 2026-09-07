// Package report builds a plain-language, plain-text summary of a
// chrome-polish session — what was scanned, how origins were scored, and
// what removal actions were taken — meant to be saved and left with the
// person whose profile was cleaned (DESIGN.md's one-on-one tech-support
// framing: a record they can keep, not just something that happened on
// screen and vanished). Like internal/reputation, this is a pure function
// over already-collected data, with no transport or navigation dependency
// of its own.
package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/reputation"
)

// Action records one removal or deep-clean operation performed during the
// session, in the order it was carried out.
type Action struct {
	Time time.Time
	// Kind is a short label for what was done, e.g. "Removed data" or
	// "Deep cleaned site".
	Kind string
	// Target is the origin, or (for a deep clean) the site's display
	// name, that Kind was performed against.
	Target string
	// Types lists the storage types cleared (e.g. "cookies",
	// "service_workers"); left empty for a deep clean, which always
	// clears everything for the site with no per-type selection.
	Types []string
	// Err is the failure the action ended with, or nil on success.
	Err error
}

// Input is everything Generate needs: the scored scan results plus the
// running log of actions taken this session.
type Input struct {
	GeneratedAt time.Time
	// Scores is every scanned origin's reputation.Score, any order —
	// Generate sorts them itself.
	Scores []reputation.Score
	// Actions is the session's removal/deep-clean log, in the order
	// performed.
	Actions []Action
}

// Generate renders in as a plain-text report: a summary line, every
// flagged origin (score > 0) with the signals behind its score, and a
// chronological log of removal actions taken. Origins that scored 0 are
// counted but not listed individually, keeping the report readable for
// someone who isn't a technical audience.
func Generate(in Input) string {
	var b strings.Builder

	generatedAt := in.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}

	flagged, clean := splitByScore(in.Scores)
	succeeded, failed := splitByOutcome(in.Actions)

	fmt.Fprintf(&b, "Chrome Polish Session Report\n")
	fmt.Fprintf(&b, "Generated: %s\n", generatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "Summary\n")
	fmt.Fprintf(&b, "  %d origin(s) scanned: %d flagged, %d clean\n", len(in.Scores), len(flagged), len(clean))
	fmt.Fprintf(&b, "  %d action(s) taken: %d succeeded, %d failed\n", len(in.Actions), len(succeeded), len(failed))

	fmt.Fprintf(&b, "\nFlagged origins (highest score first)\n")
	if len(flagged) == 0 {
		fmt.Fprintf(&b, "  None. Every scanned origin came back clean.\n")
	}
	for _, s := range flagged {
		fmt.Fprintf(&b, "  %s — score %d\n", s.Origin, s.Total)
		for _, sig := range s.Signals {
			fmt.Fprintf(&b, "      +%d %s\n", sig.Weight, sig.Detail)
		}
	}

	fmt.Fprintf(&b, "\nActions taken\n")
	if len(in.Actions) == 0 {
		fmt.Fprintf(&b, "  None. No data was removed this session.\n")
	}
	for _, a := range in.Actions {
		line := fmt.Sprintf("  [%s] %s: %s", a.Time.Format("15:04:05"), a.Kind, a.Target)
		if len(a.Types) > 0 {
			line += fmt.Sprintf(" (%s)", strings.Join(a.Types, ", "))
		}
		if a.Err != nil {
			line += fmt.Sprintf(" — FAILED: %s", a.Err)
		}
		fmt.Fprintf(&b, "%s\n", line)
	}

	fmt.Fprintf(&b, "\nNote: scores are a triage aid to help decide what to look at, not a\n")
	fmt.Fprintf(&b, "verdict — a flagged origin isn't necessarily malicious, and a clean one\n")
	fmt.Fprintf(&b, "isn't a guarantee. Remember to turn off chrome://inspect/#remote-debugging\n")
	fmt.Fprintf(&b, "now that this session is done.\n")

	return b.String()
}

// splitByScore separates scores into flagged (Total > 0, sorted highest
// score first, origin ascending as a tiebreak) and clean (Total == 0).
func splitByScore(scores []reputation.Score) (flagged, clean []reputation.Score) {
	for _, s := range scores {
		if s.Total > 0 {
			flagged = append(flagged, s)
		} else {
			clean = append(clean, s)
		}
	}
	sort.Slice(flagged, func(i, j int) bool {
		if flagged[i].Total != flagged[j].Total {
			return flagged[i].Total > flagged[j].Total
		}
		return flagged[i].Origin < flagged[j].Origin
	})
	return flagged, clean
}

func splitByOutcome(actions []Action) (succeeded, failed []Action) {
	for _, a := range actions {
		if a.Err != nil {
			failed = append(failed, a)
		} else {
			succeeded = append(succeeded, a)
		}
	}
	return succeeded, failed
}
