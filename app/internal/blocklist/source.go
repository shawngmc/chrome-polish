// Package blocklist manages a collection of named blocklist sources (a
// URL to periodically re-fetch, or a local file to periodically re-read),
// caches their raw content on disk, and compiles the enabled ones into a
// single reputation.Blocklist for scoring. It is the stateful, I/O-doing
// counterpart to reputation, which stays a pure function over
// already-loaded data.
package blocklist

import "time"

// Kind is how a Source's content is obtained.
type Kind string

const (
	KindURL  Kind = "url"
	KindFile Kind = "file"
)

// Source is one blocklist a person has added: a URL to fetch or a local
// file to read, plus enough bookkeeping to show it in the UI and refresh
// it later. Content itself is cached separately, keyed by ID (see
// manager.go) — Source only carries metadata, so the index file it's
// persisted in stays small even when the cached content is large.
type Source struct {
	ID          string    // generated once at Add time, filesystem-safe
	Name        string    // display name, user-editable
	Kind        Kind      // KindURL or KindFile
	Location    string    // the URL, or the absolute local file path
	Enabled     bool      // excluded from Compiled() when false
	LastFetched time.Time // zero value: never successfully fetched
	EntryCount  int       // number of hostnames parsed from the last successful fetch
	LastError   string    // non-fatal: the last refresh attempt's error, if any
}
