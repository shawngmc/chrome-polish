package blocklist

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// indexFile and cacheDir are relative to the Manager's baseDir (normally
// the app's per-user storage directory, e.g. fyne's app.Storage().RootURI().Path()).
const (
	indexFile = "blocklists/index.json"
	cacheDir  = "blocklists/cache"
)

// loadIndex reads the persisted source list. A missing or corrupt index
// file is not an error a caller needs to handle specially — it just means
// "no sources yet" (degrade gracefully rather than fail startup over a
// bookkeeping file).
func loadIndex(baseDir string) []Source {
	data, err := os.ReadFile(filepath.Join(baseDir, indexFile))
	if err != nil {
		return nil
	}
	var sources []Source
	if err := json.Unmarshal(data, &sources); err != nil {
		return nil
	}
	return sources
}

// saveIndex persists the source list, creating the containing directory
// if needed.
func saveIndex(baseDir string, sources []Source) error {
	path := filepath.Join(baseDir, indexFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sources, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// cachePath returns where a source's raw fetched content is cached.
func cachePath(baseDir, id string) string {
	return filepath.Join(baseDir, cacheDir, id+".txt")
}

// loadCache reads a source's cached raw content. A missing or unreadable
// cache file returns (nil, false) rather than an error — the source's
// metadata (URL/path/settings) still came from the index and stays valid;
// it just has nothing to contribute until refreshed.
func loadCache(baseDir, id string) ([]byte, bool) {
	data, err := os.ReadFile(cachePath(baseDir, id))
	if err != nil {
		return nil, false
	}
	return data, true
}

// saveCache writes a source's raw fetched content, creating the cache
// directory if needed.
func saveCache(baseDir, id string, data []byte) error {
	path := cachePath(baseDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// removeCache deletes a source's cached content, if any. A missing file
// is not an error.
func removeCache(baseDir, id string) error {
	err := os.Remove(cachePath(baseDir, id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
