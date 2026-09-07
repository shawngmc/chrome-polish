// Package removal implements DESIGN.md section 4.3: precise, per-origin
// storage removal via Chrome's own Storage.clearDataForOrigin — the same
// supported mechanism DevTools itself uses. No file-level edits, no
// LevelDB parsing, no profile copying, and no page from the origin is
// loaded to do this.
package removal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

// StorageType is one category of per-origin browser storage, matching
// Storage.clearDataForOrigin's storageTypes values.
type StorageType string

const (
	Cookies        StorageType = "cookies"
	LocalStorage   StorageType = "local_storage"
	IndexedDB      StorageType = "indexeddb"
	CacheStorage   StorageType = "cache_storage"
	ServiceWorkers StorageType = "service_workers"
	FileSystems    StorageType = "file_systems"
	WebSQL         StorageType = "websql"
)

// DefaultStorageTypes are the storage categories a scareware/malvertising
// origin actually relies on: cookies and local/indexed storage for
// tracking and state, service workers and cache storage for
// push-notification spam and offline persistence.
var DefaultStorageTypes = []StorageType{
	Cookies, ServiceWorkers, CacheStorage, IndexedDB, LocalStorage,
}

// AllStorageTypes are every type Storage.clearDataForOrigin accepts that
// this package models, for callers that want to offer the full set as
// options (e.g. a checklist) rather than just the defaults.
var AllStorageTypes = []StorageType{
	Cookies, LocalStorage, IndexedDB, CacheStorage, ServiceWorkers, FileSystems, WebSQL,
}

// ClearOrigin clears the given storage types for origin. types must be
// non-empty — callers should not invoke this with nothing selected.
//
// Like ServiceWorker.enable (see cdp.AttachTarget), Storage.clearDataForOrigin
// fails with a generic internal error when called directly on the
// browser-level connection — at least under the flag-less
// chrome://inspect/#remote-debugging toggle. Attaching to a throwaway
// about:blank target for a session to issue it from avoids that.
//
// Storage.clearDataForOrigin only clears origin's *unpartitioned* (default)
// storage bucket. Under Chrome's storage partitioning / CHIPS, a cookie can
// live in a bucket keyed by (origin, top-level site) instead — e.g. a
// third-party embed's partitioned cookie, or a CHIPS cookie set directly on
// the origin — and clearDataForOrigin silently leaves those in place, so a
// "successful" clear can still leave cookies behind (chrome://settings/
// content/all will keep showing them, marked "Partitioned"). Cookies are
// therefore cleared separately via Storage.getCookies + Network.deleteCookies
// per matching cookie, passing through its partitionKey when present, which
// reaches the actual cookie jar entry regardless of partitioning.
func ClearOrigin(ctx context.Context, client *cdp.Client, origin string, types []StorageType) error {
	if len(types) == 0 {
		return fmt.Errorf("removal: no storage types specified for %s", origin)
	}

	sessionID, cleanup, err := cdp.AttachTarget(ctx, client, "about:blank", true)
	if err != nil {
		return fmt.Errorf("removal: %w", err)
	}
	defer cleanup()

	clearCookies, rest := splitCookies(types)

	if clearCookies {
		if err := clearCookiesForOrigin(ctx, client, sessionID, origin); err != nil {
			return fmt.Errorf("removal: %w", err)
		}
	}

	if len(rest) == 0 {
		return nil
	}

	names := make([]string, len(rest))
	for i, t := range rest {
		names[i] = string(t)
	}

	if err := client.CallSession(ctx, sessionID, "Storage.clearDataForOrigin", map[string]any{
		"origin":       origin,
		"storageTypes": strings.Join(names, ","),
	}, nil); err != nil {
		return fmt.Errorf("removal: clear %s: %w", origin, err)
	}
	return nil
}

// splitCookies pulls Cookies out of types (if present), returning whether it
// was there and the remaining types for Storage.clearDataForOrigin.
func splitCookies(types []StorageType) (cookies bool, rest []StorageType) {
	for _, t := range types {
		if t == Cookies {
			cookies = true
			continue
		}
		rest = append(rest, t)
	}
	return cookies, rest
}

// cdpCookieForDelete is the subset of Storage.getCookies' Cookie fields
// needed to identify and delete one cookie via Network.deleteCookies.
// partitionKey is passed through opaquely (as whatever shape Chrome
// returned) rather than modeled, since its schema has changed across
// protocol versions and doesn't need interpreting here.
type cdpCookieForDelete struct {
	Name         string          `json:"name"`
	Domain       string          `json:"domain"`
	Path         string          `json:"path"`
	PartitionKey json.RawMessage `json:"partitionKey,omitempty"`
}

type getCookiesForDeleteResult struct {
	Cookies []cdpCookieForDelete `json:"cookies"`
}

// cookieMatchesHost reports whether a cookie's domain attribute (as returned
// by Storage.getCookies) covers host, matching either the exact host or, for
// a domain cookie (Domain set on a parent domain, e.g. ".example.com"), any
// subdomain of it.
func cookieMatchesHost(domain, host string) bool {
	d := strings.TrimPrefix(domain, ".")
	return d == host || strings.HasSuffix(host, "."+d)
}

// clearCookiesForOrigin deletes every cookie (partitioned or not) whose
// domain matches origin's host, via Storage.getCookies to enumerate the
// whole browser-wide cookie jar (there's no per-origin filter on that call)
// followed by one Network.deleteCookies per match.
func clearCookiesForOrigin(ctx context.Context, client *cdp.Client, sessionID, origin string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid origin %q", origin)
	}
	host := u.Hostname()

	var result getCookiesForDeleteResult
	if err := client.CallSession(ctx, sessionID, "Storage.getCookies", nil, &result); err != nil {
		return fmt.Errorf("Storage.getCookies: %w", err)
	}

	for _, c := range result.Cookies {
		if !cookieMatchesHost(c.Domain, host) {
			continue
		}

		params := map[string]any{
			"name":   c.Name,
			"domain": c.Domain,
			"path":   c.Path,
		}
		if len(c.PartitionKey) > 0 {
			params["partitionKey"] = c.PartitionKey
		}

		if err := client.CallSession(ctx, sessionID, "Network.deleteCookies", params, nil); err != nil {
			return fmt.Errorf("Network.deleteCookies %s@%s: %w", c.Name, c.Domain, err)
		}
	}
	return nil
}

// Result is the outcome of clearing one origin as part of a batch.
type Result struct {
	Origin string
	Err    error
}

// ClearOrigins clears types for each of origins in turn over the same
// client, continuing past individual failures so one bad origin doesn't
// abandon the rest of a multi-select removal.
func ClearOrigins(ctx context.Context, client *cdp.Client, origins []string, types []StorageType) []Result {
	results := make([]Result, len(origins))
	for i, o := range origins {
		results[i] = Result{Origin: o, Err: ClearOrigin(ctx, client, o, types)}
	}
	return results
}
