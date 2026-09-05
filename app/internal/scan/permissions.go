package scan

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

// PermissionStatus is an origin's grant for one permission category, as
// shown on its chrome://settings/content/<category> page.
type PermissionStatus string

const (
	PermissionAllow       PermissionStatus = "allow"
	PermissionBlock       PermissionStatus = "block"
	PermissionSessionOnly PermissionStatus = "session-only"
)

// PermissionCategory identifies one of Chrome's per-category site settings
// pages (chrome://settings/content/<category>) and how to interpret its
// section headers. Category is a stable internal key (used to key results
// and, later, UI columns) — Chrome's actual on-page wording lives only in
// HeaderStatus.
type PermissionCategory struct {
	Category     string
	URL          string
	HeaderStatus map[string]PermissionStatus
}

// notificationHeaders, cameraHeaders, and micHeaders were captured live
// from a running Chrome 152 instance via dom-probe (see DESIGN.md section
// 4.2 on this page's structure being Chrome's internal settings UI, not a
// stable public API). Microphone wasn't directly confirmed the same way —
// it's inferred from camera's pattern ("Allowed/Not allowed to use your
// <category>"), which is generated from the same Chromium template — but
// an unrecognized header is simply skipped (see sectionStatus), so a wrong
// guess here just means an empty result for that category, not a crash or
// a misreported status.
var (
	CategoryNotifications = PermissionCategory{
		Category: "notifications",
		URL:      "chrome://settings/content/notifications",
		HeaderStatus: map[string]PermissionStatus{
			"Allowed to send notifications":     PermissionAllow,
			"Not allowed to send notifications": PermissionBlock,
			"Clear on exit":                     PermissionSessionOnly,
		},
	}
	CategoryCamera = PermissionCategory{
		Category: "camera",
		URL:      "chrome://settings/content/camera",
		HeaderStatus: map[string]PermissionStatus{
			"Allowed to use your camera":     PermissionAllow,
			"Not allowed to use your camera": PermissionBlock,
			"Clear on exit":                  PermissionSessionOnly,
		},
	}
	CategoryMicrophone = PermissionCategory{
		Category: "microphone",
		URL:      "chrome://settings/content/microphone",
		HeaderStatus: map[string]PermissionStatus{
			"Allowed to use your microphone":     PermissionAllow,
			"Not allowed to use your microphone": PermissionBlock,
			"Clear on exit":                      PermissionSessionOnly,
		},
	}
)

// DefaultPermissionCategories are the categories DESIGN.md section 4.4
// calls out as the most relevant reputation signals: granted notification
// permission is the primary scareware/push-spam indicator, and camera/mic
// are the most sensitive permissions a compromised site could hold.
var DefaultPermissionCategories = []PermissionCategory{
	CategoryNotifications,
	CategoryCamera,
	CategoryMicrophone,
}

// PermissionGrant is one origin's grant for one permission category.
type PermissionGrant struct {
	Origin   string
	Category string
	Status   PermissionStatus
}

// DiscoverPermissions reads per-origin permission grants for category from
// its chrome://settings/content/<category> page — a local, static Chrome
// page, never a candidate/suspect origin.
func DiscoverPermissions(ctx context.Context, client *cdp.Client, category PermissionCategory) ([]PermissionGrant, error) {
	root, err := DumpDOM(ctx, client, category.URL, 3*time.Second, true)
	if err != nil {
		return nil, fmt.Errorf("scan: read %s permissions: %w", category.Category, err)
	}

	var results []PermissionGrant
	for _, siteList := range FindAll(root, "site-list") {
		status, ok := sectionStatus(siteList, category.HeaderStatus)
		if !ok {
			continue
		}
		for _, entry := range FindAll(siteList, "site-list-entry") {
			origin := entryOrigin(entry)
			if origin == "" {
				continue
			}
			results = append(results, PermissionGrant{Origin: origin, Category: category.Category, Status: status})
		}
	}
	return results, nil
}

// DiscoverAllPermissions runs DiscoverPermissions for each of categories in
// turn, over the same client (so no additional connection approval), and
// returns origin -> category -> status. A failure on one category is
// returned immediately rather than partially populating the map, since a
// caller checking map[origin][category] can't otherwise distinguish
// "checked, no grant" from "never checked".
func DiscoverAllPermissions(ctx context.Context, client *cdp.Client, categories []PermissionCategory) (map[string]map[string]PermissionStatus, error) {
	results := make(map[string]map[string]PermissionStatus)
	for _, category := range categories {
		grants, err := DiscoverPermissions(ctx, client, category)
		if err != nil {
			return nil, err
		}
		for _, g := range grants {
			if results[g.Origin] == nil {
				results[g.Origin] = make(map[string]PermissionStatus)
			}
			results[g.Origin][g.Category] = g.Status
		}
	}
	return results, nil
}

// sectionStatus maps a site-list's section header text to a
// PermissionStatus using headerStatus. This page's structure is Chrome's
// internal settings UI, not a stable public API (DESIGN.md section 4.2) —
// an unrecognized header (from a Chrome version, or category, with
// different wording) is simply skipped rather than guessed, so a mismatch
// degrades to "this data point is unavailable" rather than misreporting a
// status.
func sectionStatus(siteList *DOMNode, headerStatus map[string]PermissionStatus) (PermissionStatus, bool) {
	headers := FindAll(siteList, "h3")
	if len(headers) == 0 {
		return "", false
	}
	status, ok := headerStatus[strings.TrimSpace(headers[0].AllText())]
	return status, ok
}

// entryOrigin finds a site-list-entry's origin from its
// span.url-directionality text (e.g. "https://claude.ai:443") and
// normalizes it by dropping a default port, matching the origin format
// DiscoverOrigins produces.
func entryOrigin(entry *DOMNode) string {
	for _, span := range FindAll(entry, "span") {
		if !span.HasClass("url-directionality") {
			continue
		}
		if origin := normalizeOrigin(strings.TrimSpace(span.AllText())); origin != "" {
			return origin
		}
	}
	return ""
}

func normalizeOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return ""
	}

	host := u.Hostname()
	port := u.Port()
	isDefaultPort := (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80")
	if port != "" && !isDefaultPort {
		host += ":" + port
	}

	return u.Scheme + "://" + host
}
