// Package removal implements DESIGN.md section 4.3: precise, per-origin
// storage removal via Chrome's own Storage.clearDataForOrigin — the same
// supported mechanism DevTools itself uses. No file-level edits, no
// LevelDB parsing, no profile copying, and no page from the origin is
// loaded to do this.
package removal

import (
	"context"
	"fmt"
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
func ClearOrigin(ctx context.Context, client *cdp.Client, origin string, types []StorageType) error {
	if len(types) == 0 {
		return fmt.Errorf("removal: no storage types specified for %s", origin)
	}

	sessionID, cleanup, err := cdp.AttachTarget(ctx, client, "about:blank", true)
	if err != nil {
		return fmt.Errorf("removal: %w", err)
	}
	defer cleanup()

	names := make([]string, len(types))
	for i, t := range types {
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
