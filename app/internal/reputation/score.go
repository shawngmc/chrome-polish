// Package reputation implements the heuristic scoring described in
// DESIGN.md section 4.4: a pure function over already-collected data
// (candidate origins and their permission grants), with no transport or
// navigation dependency of its own.
//
// Known gap: DESIGN.md's primary heuristic is "granted notifications +
// low visit signal" together. This package scores granted notifications
// on its own — a "visit count" signal isn't implemented yet, since Chrome
// doesn't expose it over CDP, and reading it would mean scraping
// chrome://history or the all-sites page's engagement ordering, which is
// a separate, heavier piece of work (and browsing history is
// considerably more sensitive to read wholesale than what origin
// discovery and permission reads already touch).
package reputation

import (
	"net"
	"strings"

	"github.com/shawngmc/chrome-polish/app/internal/scan"
)

// Signal is one heuristic that contributed to an origin's score.
type Signal struct {
	Name   string
	Weight int
	Detail string
}

// SignalBlocklist is the Signal.Name Compute uses for a blocklist hit —
// exported so callers (e.g. the UI) can check for it by name instead of a
// string literal.
const SignalBlocklist = "blocklist"

// Score is an origin's total reputation score and the signals behind it.
// It is a triage aid for a person to review, not a verdict — nothing here
// removes or blocks anything on its own.
type Score struct {
	Origin  string
	Total   int
	Signals []Signal
}

// Blocklist is a set of operator-supplied hostnames (or bare suffixes,
// e.g. "example.com" also matching "sub.example.com") to always flag.
// Keys are lowercase.
type Blocklist map[string]bool

// abusedTLDs is a small, curated starting list of TLDs with a track
// record of disproportionate abuse by malvertising/scareware operators.
// It's a coarse, adjustable heuristic, not an authoritative or exhaustive
// list — expect to tune it as real sessions turn up false
// positives/negatives.
var abusedTLDs = map[string]bool{
	"tk": true, "ml": true, "ga": true, "cf": true, "gq": true,
	"top": true, "xyz": true, "click": true, "link": true,
	"work": true, "men": true, "date": true, "stream": true,
	"racing": true, "review": true, "party": true, "trade": true,
	"accountant": true, "science": true, "faith": true, "cricket": true,
	"loan": true, "win": true, "bid": true, "download": true,
}

// suspiciousKeywords is a small, curated starting list of hostname
// substrings common in scareware/push-spam and tech-support-scam domains
// (fake update/security prompts, prize/gift lures). Also an adjustable
// heuristic, not exhaustive.
var suspiciousKeywords = []string{
	"update", "alert", "security-check", "virus", "warning",
	"verify-account", "confirm-account", "flashplayer", "flash-player",
	"browser-update", "codec-update", "player-update", "you-won",
	"claim-your", "free-gift", "win-prize", "congratulations",
}

// Compute scores a single origin against its permission grants
// (category -> status, as produced by scan.DiscoverAllPermissions) and an
// optional blocklist (nil is fine).
func Compute(origin scan.Origin, permissions map[string]scan.PermissionStatus, blocklist Blocklist) Score {
	var signals []Signal
	host := hostname(origin.Origin)

	if matchesBlocklist(host, blocklist) {
		signals = append(signals, Signal{
			Name:   SignalBlocklist,
			Weight: 100,
			Detail: "hostname is on the user-supplied blocklist",
		})
	}

	if permissions[scan.CategoryNotifications.Category] == scan.PermissionAllow {
		signals = append(signals, Signal{
			Name:   "notifications-allowed",
			Weight: 40,
			Detail: "notification permission is granted",
		})
	}
	if permissions[scan.CategoryCamera.Category] == scan.PermissionAllow {
		signals = append(signals, Signal{
			Name:   "camera-allowed",
			Weight: 15,
			Detail: "camera permission is granted",
		})
	}
	if permissions[scan.CategoryMicrophone.Category] == scan.PermissionAllow {
		signals = append(signals, Signal{
			Name:   "microphone-allowed",
			Weight: 15,
			Detail: "microphone permission is granted",
		})
	}

	if tld := lastLabel(host); tld != "" && abusedTLDs[tld] {
		signals = append(signals, Signal{
			Name:   "abused-tld",
			Weight: 25,
			Detail: "top-level domain ." + tld + " has a history of abuse",
		})
	}

	for _, kw := range suspiciousKeywords {
		if strings.Contains(host, kw) {
			signals = append(signals, Signal{
				Name:   "suspicious-keyword",
				Weight: 20,
				Detail: `hostname contains "` + kw + `"`,
			})
		}
	}

	total := 0
	for _, s := range signals {
		total += s.Weight
	}

	return Score{Origin: origin.Origin, Total: total, Signals: signals}
}

// ComputeAll scores every origin in order, looking up each one's
// permissions from permissions (origin -> category -> status, as produced
// by scan.DiscoverAllPermissions).
func ComputeAll(origins []scan.Origin, permissions map[string]map[string]scan.PermissionStatus, blocklist Blocklist) []Score {
	scores := make([]Score, len(origins))
	for i, o := range origins {
		scores[i] = Compute(o, permissions[o.Origin], blocklist)
	}
	return scores
}

// hostname extracts the host (no scheme, no port) from an origin string
// such as "https://example.com" or "https://example.com:8443".
func hostname(origin string) string {
	host := origin
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	return strings.ToLower(host)
}

func lastLabel(host string) string {
	if i := strings.LastIndex(host, "."); i >= 0 {
		return host[i+1:]
	}
	return ""
}

func matchesBlocklist(host string, blocklist Blocklist) bool {
	if blocklist == nil {
		return false
	}
	for suffix := host; ; {
		if blocklist[suffix] {
			return true
		}
		i := strings.Index(suffix, ".")
		if i < 0 {
			return false
		}
		suffix = suffix[i+1:]
	}
}

// ParseBlocklist parses a blocklist in either of two common formats: a
// bare hostname (or suffix, e.g. "example.com") per line, or a classic
// hosts file line ("0.0.0.0 example.com", optionally with more than one
// hostname after the IP) — the format most public malware/scam domain
// lists (e.g. StevenBlack/hosts, URLhaus) actually ship in. Blank lines,
// lines starting with "#", and inline "# ..." comments are ignored.
func ParseBlocklist(data []byte) Blocklist {
	bl := make(Blocklist)
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		hosts := fields
		if net.ParseIP(fields[0]) != nil {
			hosts = fields[1:] // hosts-file line: drop the leading IP
		}
		for _, h := range hosts {
			bl[strings.ToLower(h)] = true
		}
	}
	return bl
}
