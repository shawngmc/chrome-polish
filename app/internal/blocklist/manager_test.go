package blocklist

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// stubFetch returns a fetchFunc that always returns data, or err if set.
func stubFetch(data []byte, err error) fetchFunc {
	return func(ctx context.Context, url string) ([]byte, error) {
		return data, err
	}
}

func TestManagerAddURLFetchesAndPersists(t *testing.T) {
	m := NewManager(t.TempDir())
	m.fetch = stubFetch([]byte("evil.com\nbad.example.net\n"), nil)

	src, err := m.AddURL(context.Background(), "https://example.com/list.txt", "test list")
	if err != nil {
		t.Fatalf("AddURL: %v", err)
	}
	if src.EntryCount != 2 {
		t.Errorf("EntryCount = %d, want 2", src.EntryCount)
	}
	if src.LastFetched.IsZero() {
		t.Error("LastFetched should be set after a successful AddURL")
	}

	sources := m.Sources()
	if len(sources) != 1 {
		t.Fatalf("got %d sources, want 1", len(sources))
	}

	compiled := m.Compiled()
	if !compiled["evil.com"] || !compiled["bad.example.net"] {
		t.Errorf("Compiled() = %+v, missing expected hosts", compiled)
	}
}

func TestManagerAddURLRejectsInvalidURL(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.AddURL(context.Background(), "not a url", "bad"); err == nil {
		t.Error("expected an error for an invalid URL, got nil")
	}
	if len(m.Sources()) != 0 {
		t.Error("a failed AddURL should not add a source")
	}
}

func TestManagerAddURLFetchFailureAddsNothing(t *testing.T) {
	m := NewManager(t.TempDir())
	m.fetch = stubFetch(nil, errors.New("network down"))

	if _, err := m.AddURL(context.Background(), "https://example.com/list.txt", "test"); err == nil {
		t.Error("expected an error from a failing fetch, got nil")
	}
	if len(m.Sources()) != 0 {
		t.Error("a failed fetch should not add a source")
	}
}

func TestManagerPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()

	m1 := NewManager(dir)
	m1.fetch = stubFetch([]byte("evil.com\n"), nil)
	if _, err := m1.AddURL(context.Background(), "https://example.com/list.txt", "test list"); err != nil {
		t.Fatalf("AddURL: %v", err)
	}

	m2 := NewManager(dir)
	if err := m2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	sources := m2.Sources()
	if len(sources) != 1 || sources[0].Name != "test list" {
		t.Fatalf("got sources %+v, want one named %q", sources, "test list")
	}
	if !m2.Compiled()["evil.com"] {
		t.Errorf("Compiled() = %+v, missing evil.com after reload", m2.Compiled())
	}
}

func TestManagerRefreshFailurePreservesOldContent(t *testing.T) {
	m := NewManager(t.TempDir())
	m.fetch = stubFetch([]byte("evil.com\n"), nil)
	src, err := m.AddURL(context.Background(), "https://example.com/list.txt", "test")
	if err != nil {
		t.Fatalf("AddURL: %v", err)
	}
	firstFetched := src.LastFetched

	m.fetch = stubFetch(nil, errors.New("network down"))
	if err := m.Refresh(context.Background(), src.ID); err == nil {
		t.Error("expected Refresh to report the fetch failure")
	}

	sources := m.Sources()
	if sources[0].LastError == "" {
		t.Error("LastError should be set after a failed refresh")
	}
	if !sources[0].LastFetched.Equal(firstFetched) {
		t.Errorf("LastFetched changed on a failed refresh: got %v, want %v", sources[0].LastFetched, firstFetched)
	}
	if !m.Compiled()["evil.com"] {
		t.Error("a failed refresh should not clear previously-cached content")
	}
}

func TestManagerRefreshSuccessUpdatesContent(t *testing.T) {
	m := NewManager(t.TempDir())
	m.fetch = stubFetch([]byte("evil.com\n"), nil)
	src, err := m.AddURL(context.Background(), "https://example.com/list.txt", "test")
	if err != nil {
		t.Fatalf("AddURL: %v", err)
	}

	m.fetch = stubFetch([]byte("evil.com\nnew-bad.com\nanother.com\n"), nil)
	if err := m.Refresh(context.Background(), src.ID); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	sources := m.Sources()
	if sources[0].EntryCount != 3 {
		t.Errorf("EntryCount = %d, want 3", sources[0].EntryCount)
	}
	if !m.Compiled()["new-bad.com"] {
		t.Error("Compiled() should reflect the refreshed content")
	}
}

func TestManagerSetEnabledExcludesFromCompiled(t *testing.T) {
	m := NewManager(t.TempDir())
	m.fetch = stubFetch([]byte("evil.com\n"), nil)
	src, err := m.AddURL(context.Background(), "https://example.com/list.txt", "test")
	if err != nil {
		t.Fatalf("AddURL: %v", err)
	}

	if err := m.SetEnabled(src.ID, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if len(m.Compiled()) != 0 {
		t.Errorf("Compiled() = %+v, want empty once source is disabled", m.Compiled())
	}

	if err := m.SetEnabled(src.ID, true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if !m.Compiled()["evil.com"] {
		t.Error("Compiled() should include the source again once re-enabled")
	}
}

func TestManagerRemove(t *testing.T) {
	m := NewManager(t.TempDir())
	m.fetch = stubFetch([]byte("evil.com\n"), nil)
	src, err := m.AddURL(context.Background(), "https://example.com/list.txt", "test")
	if err != nil {
		t.Fatalf("AddURL: %v", err)
	}

	if err := m.Remove(src.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(m.Sources()) != 0 {
		t.Error("source should be gone after Remove")
	}
	if len(m.Compiled()) != 0 {
		t.Error("Compiled() should be empty after removing the only source")
	}
	if err := m.Remove(src.ID); err == nil {
		t.Error("removing an already-removed source should error, not silently succeed")
	}
}

func TestManagerAddFileAndRefreshFromPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "list.txt")
	if err := os.WriteFile(path, []byte("evil.com\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m := NewManager(t.TempDir())
	src, err := m.AddFile("local list", path)
	if err != nil {
		t.Fatalf("AddFile: %v", err)
	}
	if src.EntryCount != 1 {
		t.Errorf("EntryCount = %d, want 1", src.EntryCount)
	}

	if err := os.WriteFile(path, []byte("evil.com\nnew.com\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := m.Refresh(context.Background(), src.ID); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !m.Compiled()["new.com"] {
		t.Error("Refresh should re-read the file's current contents")
	}
}

func TestManagerAddFileMissingPathFails(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.AddFile("gone", filepath.Join(t.TempDir(), "does-not-exist.txt")); err == nil {
		t.Error("expected an error adding a nonexistent file")
	}
}

func TestManagerLoadDegradesOnCorruptIndex(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, indexFile)
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(indexPath, []byte("not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m := NewManager(dir)
	if err := m.Load(); err != nil {
		t.Fatalf("Load should degrade gracefully on a corrupt index, got error: %v", err)
	}
	if len(m.Sources()) != 0 {
		t.Errorf("got %d sources from a corrupt index, want 0", len(m.Sources()))
	}
}

func TestManagerLoadDegradesOnMissingCacheFile(t *testing.T) {
	dir := t.TempDir()
	m1 := NewManager(dir)
	m1.fetch = stubFetch([]byte("evil.com\n"), nil)
	src, err := m1.AddURL(context.Background(), "https://example.com/list.txt", "test")
	if err != nil {
		t.Fatalf("AddURL: %v", err)
	}

	if err := os.Remove(cachePath(dir, src.ID)); err != nil {
		t.Fatalf("Remove cache file: %v", err)
	}

	m2 := NewManager(dir)
	if err := m2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m2.Sources()) != 1 {
		t.Fatalf("got %d sources, want 1 (metadata should survive a missing cache file)", len(m2.Sources()))
	}
	if len(m2.Compiled()) != 0 {
		t.Errorf("Compiled() = %+v, want empty when the cache file is missing", m2.Compiled())
	}
}

func TestManagerCompiledUnionsMultipleEnabledSources(t *testing.T) {
	m := NewManager(t.TempDir())

	m.fetch = stubFetch([]byte("a.com\nshared.com\n"), nil)
	if _, err := m.AddURL(context.Background(), "https://example.com/one.txt", "one"); err != nil {
		t.Fatalf("AddURL: %v", err)
	}
	m.fetch = stubFetch([]byte("b.com\nshared.com\n"), nil)
	if _, err := m.AddURL(context.Background(), "https://example.com/two.txt", "two"); err != nil {
		t.Fatalf("AddURL: %v", err)
	}

	compiled := m.Compiled()
	if len(compiled) != 3 {
		t.Errorf("Compiled() = %+v, want 3 unique hosts", compiled)
	}
	for _, host := range []string{"a.com", "b.com", "shared.com"} {
		if !compiled[host] {
			t.Errorf("Compiled() missing %q", host)
		}
	}
}
