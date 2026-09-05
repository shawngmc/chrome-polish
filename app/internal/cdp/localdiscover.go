package cdp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Browser identifies a Chromium-based browser for the purpose of locating
// its default user-data directory.
type Browser string

const (
	BrowserChrome  Browser = "chrome"
	BrowserBrave   Browser = "brave"
	BrowserEdge    Browser = "edge"
	BrowserVivaldi Browser = "vivaldi"
)

// userDataDirNames gives each browser's directory name(s) under the
// per-OS base directory returned by userDataBaseDir.
var userDataDirNames = map[Browser]map[string]string{
	BrowserChrome: {
		"darwin":  "Google/Chrome",
		"linux":   "google-chrome",
		"windows": `Google\Chrome\User Data`,
	},
	BrowserBrave: {
		"darwin":  "BraveSoftware/Brave-Browser",
		"linux":   "BraveSoftware/Brave-Browser",
		"windows": `BraveSoftware\Brave-Browser\User Data`,
	},
	BrowserEdge: {
		"darwin":  "Microsoft Edge",
		"linux":   "microsoft-edge",
		"windows": `Microsoft\Edge\User Data`,
	},
	BrowserVivaldi: {
		"darwin":  "Vivaldi",
		"linux":   "vivaldi",
		"windows": `Vivaldi\User Data`,
	},
}

// UserDataDir returns the default user-data directory for browser on the
// current OS. This is a best-effort default; a person who moved their
// profile with a custom --user-data-dir won't be found here.
func UserDataDir(browser Browser) (string, error) {
	names, ok := userDataDirNames[browser]
	if !ok {
		return "", fmt.Errorf("cdp: unsupported browser %q", browser)
	}
	name, ok := names[runtime.GOOS]
	if !ok {
		return "", fmt.Errorf("cdp: %s is not supported on %s", browser, runtime.GOOS)
	}

	base, err := userDataBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, name), nil
}

// userDataBaseDir returns the OS-specific directory that browser user-data
// directories live under (e.g. "~/Library/Application Support" on macOS).
func userDataBaseDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cdp: resolve home directory: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	case "linux":
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return xdg, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cdp: resolve home directory: %w", err)
		}
		return filepath.Join(home, ".config"), nil
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return local, nil
		}
		return "", fmt.Errorf("cdp: LOCALAPPDATA is not set")
	default:
		return "", fmt.Errorf("cdp: unsupported OS %s", runtime.GOOS)
	}
}

// DiscoverLocal finds the browser-level websocket debugger URL for a
// Chromium-based browser running on this machine with remote debugging
// enabled via chrome://inspect/#remote-debugging. It reads the
// DevToolsActivePort file the browser writes to its user-data directory,
// which is how that flag-less toggle exposes its endpoint (unlike the
// classic --remote-debugging-port flag, it does not serve an HTTP
// /json/version discovery endpoint).
func DiscoverLocal(browser Browser) (string, error) {
	dir, err := UserDataDir(browser)
	if err != nil {
		return "", err
	}
	return readDevToolsActivePort(dir)
}

func readDevToolsActivePort(userDataDir string) (string, error) {
	path := filepath.Join(userDataDir, "DevToolsActivePort")

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cdp: read %s: %w (is remote debugging enabled for this browser instance?)", path, err)
	}

	lines := strings.SplitN(strings.TrimRight(string(data), "\n"), "\n", 2)
	if len(lines) != 2 {
		return "", fmt.Errorf("cdp: %s has unexpected contents", path)
	}
	port := strings.TrimSpace(lines[0])
	wsPath := strings.TrimSpace(lines[1])
	if port == "" || wsPath == "" {
		return "", fmt.Errorf("cdp: %s has unexpected contents", path)
	}

	return fmt.Sprintf("ws://127.0.0.1:%s%s", port, wsPath), nil
}
