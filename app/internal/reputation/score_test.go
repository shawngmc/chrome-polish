package reputation

import (
	"testing"

	"github.com/shawngmc/chrome-polish/app/internal/scan"
)

func TestCompute(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		permissions map[string]scan.PermissionStatus
		blocklist   Blocklist
		wantTotal   int
		wantSignals []string // signal names expected, order-independent
	}{
		{
			name:        "clean origin, no signals",
			origin:      "https://example.com",
			wantTotal:   0,
			wantSignals: nil,
		},
		{
			name:   "notifications allowed",
			origin: "https://example.com",
			permissions: map[string]scan.PermissionStatus{
				scan.CategoryNotifications.Category: scan.PermissionAllow,
			},
			wantTotal:   40,
			wantSignals: []string{"notifications-allowed"},
		},
		{
			name:   "notifications blocked does not score",
			origin: "https://example.com",
			permissions: map[string]scan.PermissionStatus{
				scan.CategoryNotifications.Category: scan.PermissionBlock,
			},
			wantTotal:   0,
			wantSignals: nil,
		},
		{
			name:   "camera and microphone allowed",
			origin: "https://example.com",
			permissions: map[string]scan.PermissionStatus{
				scan.CategoryCamera.Category:     scan.PermissionAllow,
				scan.CategoryMicrophone.Category: scan.PermissionAllow,
			},
			wantTotal:   30,
			wantSignals: []string{"camera-allowed", "microphone-allowed"},
		},
		{
			name:        "abused tld",
			origin:      "https://free-prizes.top",
			wantTotal:   25,
			wantSignals: []string{"abused-tld"},
		},
		{
			name:        "legitimate tld is not flagged",
			origin:      "https://example.com",
			wantTotal:   0,
			wantSignals: nil,
		},
		{
			name:      "suspicious keyword in hostname",
			origin:    "https://flashplayer-update.example.com",
			wantTotal: 60,
			// "flashplayer", "update", and "player-update" (itself a
			// substring of "flashplayer-update") all match — overlapping
			// keyword hits are expected to stack, not dedupe.
			wantSignals: []string{"suspicious-keyword", "suspicious-keyword", "suspicious-keyword"},
		},
		{
			name:        "blocklist match on exact host",
			origin:      "https://evil.example.com",
			blocklist:   Blocklist{"evil.example.com": true},
			wantTotal:   100,
			wantSignals: []string{"blocklist"},
		},
		{
			name:        "blocklist match on parent suffix",
			origin:      "https://sub.evil.com",
			blocklist:   Blocklist{"evil.com": true},
			wantTotal:   100,
			wantSignals: []string{"blocklist"},
		},
		{
			name:        "blocklist does not match unrelated host",
			origin:      "https://example.com",
			blocklist:   Blocklist{"evil.com": true},
			wantTotal:   0,
			wantSignals: nil,
		},
		{
			name:   "multiple signals sum",
			origin: "https://virus-warning.top",
			permissions: map[string]scan.PermissionStatus{
				scan.CategoryNotifications.Category: scan.PermissionAllow,
			},
			wantTotal:   40 + 25 + 20 + 20, // notifications + abused-tld + "virus" + "warning"
			wantSignals: []string{"notifications-allowed", "abused-tld", "suspicious-keyword", "suspicious-keyword"},
		},
		{
			name:        "chrome-extension origin never crashes and scores clean",
			origin:      "chrome-extension://abcdefghijklmnopabcdefghijklmnop",
			wantTotal:   0,
			wantSignals: nil,
		},
		{
			name:      "nil blocklist is safe",
			origin:    "https://example.com",
			blocklist: nil,
			wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origin := scan.Origin{Origin: tt.origin}
			got := Compute(origin, tt.permissions, tt.blocklist)

			if got.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d (signals: %+v)", got.Total, tt.wantTotal, got.Signals)
			}
			if got.Origin != tt.origin {
				t.Errorf("Origin = %q, want %q", got.Origin, tt.origin)
			}
			if len(got.Signals) != len(tt.wantSignals) {
				t.Fatalf("got %d signals %+v, want %d (%v)", len(got.Signals), got.Signals, len(tt.wantSignals), tt.wantSignals)
			}
			gotNames := make(map[string]int)
			for _, s := range got.Signals {
				gotNames[s.Name]++
			}
			wantNames := make(map[string]int)
			for _, n := range tt.wantSignals {
				wantNames[n]++
			}
			for name, count := range wantNames {
				if gotNames[name] != count {
					t.Errorf("signal %q: got %d, want %d", name, gotNames[name], count)
				}
			}
		})
	}
}

func TestComputeAllPreservesOrderAndLength(t *testing.T) {
	origins := []scan.Origin{
		{Origin: "https://a.com"},
		{Origin: "https://b.top"},
		{Origin: "https://c.com"},
	}
	permissions := map[string]map[string]scan.PermissionStatus{
		"https://b.top": {scan.CategoryNotifications.Category: scan.PermissionAllow},
	}

	scores := ComputeAll(origins, permissions, nil)

	if len(scores) != len(origins) {
		t.Fatalf("got %d scores, want %d", len(scores), len(origins))
	}
	for i, o := range origins {
		if scores[i].Origin != o.Origin {
			t.Errorf("scores[%d].Origin = %q, want %q", i, scores[i].Origin, o.Origin)
		}
	}
	if scores[1].Total != 40+25 {
		t.Errorf("b.top score = %d, want %d", scores[1].Total, 40+25)
	}
}

func TestParseBlocklist(t *testing.T) {
	data := []byte(`
# comment line
evil.com

sub.example.com
  spaced.com
`)
	bl := ParseBlocklist(data)

	want := []string{"evil.com", "sub.example.com", "spaced.com"}
	if len(bl) != len(want) {
		t.Fatalf("got %d entries %+v, want %d", len(bl), bl, len(want))
	}
	for _, h := range want {
		if !bl[h] {
			t.Errorf("missing expected entry %q", h)
		}
	}
}

func TestParseBlocklistHostsFileFormat(t *testing.T) {
	// The format most public malware/scam domain lists actually ship in
	// (StevenBlack/hosts, URLhaus, etc.): "<ip> <hostname...>" per line,
	// often with a trailing "# ..." comment.
	data := []byte(`
# Title: Example Blocklist
0.0.0.0 evil.com
0.0.0.0 scam.example.net # known tech-support scam
127.0.0.1 alias-one.com alias-two.com
::1 ipv6-blocked.com
`)
	bl := ParseBlocklist(data)

	want := []string{"evil.com", "scam.example.net", "alias-one.com", "alias-two.com", "ipv6-blocked.com"}
	if len(bl) != len(want) {
		t.Fatalf("got %d entries %+v, want %d", len(bl), bl, len(want))
	}
	for _, h := range want {
		if !bl[h] {
			t.Errorf("missing expected entry %q", h)
		}
	}
}

func TestMatchesBlocklistDoesNotOverMatch(t *testing.T) {
	// "notevil.com" should not match a blocklist entry for "evil.com".
	bl := Blocklist{"evil.com": true}
	if matchesBlocklist("notevil.com", bl) {
		t.Error("matchesBlocklist(\"notevil.com\") should not match blocklist entry \"evil.com\"")
	}
}
