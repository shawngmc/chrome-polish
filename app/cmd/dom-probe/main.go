// Command dom-probe is a diagnostic tool: navigate a background target to
// a given local Chrome URL and dump its shadow-DOM-pierced structure.
// Intended for inspecting the current structure of internal pages (e.g.
// chrome://settings/content/all) that DESIGN.md section 4.2 notes aren't a
// stable public API and can shift across Chrome versions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
	"github.com/shawngmc/chrome-polish/app/internal/scan"
)

func main() {
	browser := flag.String("browser", "chrome", "local browser to auto-discover via its DevToolsActivePort file")
	addr := flag.String("addr", "", "host:port to discover via HTTP /json/version instead")
	wsURL := flag.String("ws", "", "connect directly to this websocket debugger URL")
	url := flag.String("url", "chrome://settings/content/all", "local Chrome URL to dump")
	settle := flag.Duration("settle", 500*time.Millisecond, "delay after navigation before reading the DOM")
	foreground := flag.Bool("foreground", false, "bring the target to the front instead of opening it in the background (needed for pages with virtualized lists)")
	timeout := flag.Duration("timeout", 20*time.Second, "overall timeout for the probe")
	out := flag.String("out", "", "write JSON to this file instead of stdout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	debuggerURL := *wsURL
	var err error
	switch {
	case debuggerURL != "":
	case *addr != "":
		var info *cdp.BrowserInfo
		info, err = cdp.DiscoverBrowser(ctx, *addr)
		if err == nil {
			debuggerURL = info.WebSocketDebuggerURL
		}
	default:
		debuggerURL, err = cdp.DiscoverLocal(cdp.Browser(*browser))
	}
	if err != nil {
		log.Fatalf("discover: %v", err)
	}

	client, err := cdp.Connect(ctx, debuggerURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Close()

	root, err := scan.DumpDOM(ctx, client, *url, *settle, *foreground)
	if err != nil {
		log.Fatalf("dump %s: %v", *url, err)
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}

	if *out != "" {
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			log.Fatalf("write %s: %v", *out, err)
		}
		return
	}
	os.Stdout.Write(data)
	os.Stdout.WriteString("\n")
}
