package blocklist

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/reputation"
)

// fetchTimeout bounds a single URL fetch. It's longer than a typical CDP
// round-trip timeout elsewhere in this app (see connectTimeout in
// ui/connect.go) because a blocklist download is a large one-shot HTTP
// GET (oisd's "big" list is on the order of tens of MB), not a local
// call.
const fetchTimeout = 30 * time.Second

// fetchFunc retrieves a URL's raw content. It's a field on Manager (not a
// bare package function) so tests can substitute a stub and exercise
// AddURL/Refresh without a real network call.
type fetchFunc func(ctx context.Context, url string) ([]byte, error)

// Manager owns a collection of blocklist Sources: it persists their
// metadata and cached content under baseDir, fetches/reads their content,
// and compiles the enabled ones into a single reputation.Blocklist for
// scoring. All exported methods are safe for concurrent use — Refresh and
// AddURL are expected to run on a background goroutine while the UI reads
// Sources()/Compiled() from the main thread.
type Manager struct {
	baseDir string
	fetch   fetchFunc

	mu       sync.Mutex
	sources  []Source
	compiled reputation.Blocklist
}

// NewManager builds a Manager rooted at baseDir (normally the app's
// per-user storage directory). Call Load to restore any previously
// persisted sources.
func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir:  baseDir,
		fetch:    defaultFetch,
		compiled: reputation.Blocklist{},
	}
}

// Load restores persisted sources and compiles their cached content. A
// missing or corrupt index, or a missing/corrupt cache file for an
// individual source, is not a fatal error — it degrades to an empty
// source list, or a source with nothing compiled until it's refreshed,
// rather than failing startup.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sources = loadIndex(m.baseDir)
	m.recompile()
	return nil
}

// Sources returns a snapshot of the currently known sources, in the order
// they were added.
func (m *Manager) Sources() []Source {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Source, len(m.sources))
	copy(out, m.sources)
	return out
}

// Compiled returns the merged blocklist of every enabled source's last
// successfully cached content.
func (m *Manager) Compiled() reputation.Blocklist {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.compiled
}

// AddURL adds a new URL-sourced blocklist, fetches it immediately to
// validate it and populate the initial cache, and returns the new Source.
// On fetch failure, no source is added.
func (m *Manager) AddURL(ctx context.Context, rawURL, name string) (Source, error) {
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return Source{}, fmt.Errorf("blocklist: invalid URL %q: %w", rawURL, err)
	}
	if name == "" {
		name = rawURL
	}

	src := Source{
		ID:       newID(),
		Name:     name,
		Kind:     KindURL,
		Location: rawURL,
		Enabled:  true,
	}
	return m.addAndFetch(ctx, src)
}

// AddFile adds a new file-sourced blocklist, reading it immediately to
// validate it and populate the initial cache, and returns the new Source.
// path should be absolute. On read failure, no source is added.
func (m *Manager) AddFile(name, path string) (Source, error) {
	if name == "" {
		name = path
	}

	src := Source{
		ID:       newID(),
		Name:     name,
		Kind:     KindFile,
		Location: path,
		Enabled:  true,
	}
	return m.addAndFetch(context.Background(), src)
}

// addAndFetch fetches/reads src's content, and only if that succeeds,
// appends it to the source list and persists everything.
func (m *Manager) addAndFetch(ctx context.Context, src Source) (Source, error) {
	data, err := m.retrieve(ctx, src)
	if err != nil {
		return Source{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	src.LastFetched = time.Now()
	src.EntryCount = len(reputation.ParseBlocklist(data))
	src.LastError = ""

	if err := saveCache(m.baseDir, src.ID, data); err != nil {
		return Source{}, fmt.Errorf("blocklist: cache %s: %w", src.Name, err)
	}
	m.sources = append(m.sources, src)
	if err := saveIndex(m.baseDir, m.sources); err != nil {
		return Source{}, fmt.Errorf("blocklist: save index: %w", err)
	}
	m.recompile()

	return src, nil
}

// Remove deletes a source and its cached content.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.indexOf(id)
	if idx < 0 {
		return fmt.Errorf("blocklist: no source with id %q", id)
	}

	m.sources = append(m.sources[:idx:idx], m.sources[idx+1:]...)
	if err := saveIndex(m.baseDir, m.sources); err != nil {
		return fmt.Errorf("blocklist: save index: %w", err)
	}
	if err := removeCache(m.baseDir, id); err != nil {
		return fmt.Errorf("blocklist: remove cache: %w", err)
	}
	m.recompile()
	return nil
}

// SetEnabled toggles whether a source contributes to Compiled().
func (m *Manager) SetEnabled(id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.indexOf(id)
	if idx < 0 {
		return fmt.Errorf("blocklist: no source with id %q", id)
	}
	m.sources[idx].Enabled = enabled
	if err := saveIndex(m.baseDir, m.sources); err != nil {
		return fmt.Errorf("blocklist: save index: %w", err)
	}
	m.recompile()
	return nil
}

// Refresh re-fetches (URL sources) or re-reads (file sources) a source's
// content. On failure, the source's previously cached content and
// LastFetched are left untouched — only LastError is updated — so a
// transient network or file-system hiccup doesn't blank out a
// previously-working list.
func (m *Manager) Refresh(ctx context.Context, id string) error {
	m.mu.Lock()
	idx := m.indexOf(id)
	if idx < 0 {
		m.mu.Unlock()
		return fmt.Errorf("blocklist: no source with id %q", id)
	}
	src := m.sources[idx]
	m.mu.Unlock()

	data, err := m.retrieve(ctx, src)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Re-resolve idx: a concurrent Remove could have dropped the source
	// while the fetch/read above was in flight.
	idx = m.indexOf(id)
	if idx < 0 {
		return fmt.Errorf("blocklist: source %q was removed during refresh", id)
	}

	if err != nil {
		m.sources[idx].LastError = err.Error()
		_ = saveIndex(m.baseDir, m.sources) // best-effort; LastError is advisory
		return err
	}

	m.sources[idx].LastFetched = time.Now()
	m.sources[idx].EntryCount = len(reputation.ParseBlocklist(data))
	m.sources[idx].LastError = ""

	if err := saveCache(m.baseDir, id, data); err != nil {
		m.sources[idx].LastError = err.Error()
		return err
	}
	if err := saveIndex(m.baseDir, m.sources); err != nil {
		return fmt.Errorf("blocklist: save index: %w", err)
	}
	m.recompile()
	return nil
}

// retrieve fetches (KindURL) or reads (KindFile) src's raw content,
// without touching Manager state.
func (m *Manager) retrieve(ctx context.Context, src Source) ([]byte, error) {
	switch src.Kind {
	case KindURL:
		return m.fetch(ctx, src.Location)
	case KindFile:
		return os.ReadFile(src.Location)
	default:
		return nil, fmt.Errorf("blocklist: unknown source kind %q", src.Kind)
	}
}

// indexOf returns the index of the source with the given ID, or -1.
// Callers must hold m.mu.
func (m *Manager) indexOf(id string) int {
	for i, s := range m.sources {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// recompile rebuilds m.compiled from every enabled source's cached
// content. Callers must hold m.mu. A source with no readable cache (never
// fetched, or its cache file went missing/corrupt) simply contributes
// nothing, rather than failing the whole merge.
func (m *Manager) recompile() {
	merged := make(reputation.Blocklist)
	for _, s := range m.sources {
		if !s.Enabled {
			continue
		}
		data, ok := loadCache(m.baseDir, s.ID)
		if !ok {
			continue
		}
		for host := range reputation.ParseBlocklist(data) {
			merged[host] = true
		}
	}
	m.compiled = merged
}

// idCounter disambiguates IDs generated within the same clock tick (some
// platforms have coarser-than-nanosecond timer resolution, and tests in
// particular can add sources back-to-back fast enough to collide).
var idCounter int64

// newID generates a unique, filesystem-safe source identifier.
func newID() string {
	n := atomic.AddInt64(&idCounter, 1)
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatInt(n, 36)
}

// defaultFetch is the real, network-backed fetchFunc.
func defaultFetch(ctx context.Context, rawURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("blocklist: build request for %s: %w", rawURL, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("blocklist: fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("blocklist: fetch %s: unexpected status %s", rawURL, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
