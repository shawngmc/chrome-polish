// Command scan-probe is a diagnostic tool: connect to a Chrome/Chromium
// instance and run origin discovery (DESIGN.md section 4.1), printing the
// candidate origins found. Uses the same connection modes as cdp-probe.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
	"github.com/shawngmc/chrome-polish/app/internal/scan"
)

func main() {
	browser := flag.String("browser", "chrome", "local browser to auto-discover via its DevToolsActivePort file (chrome, brave, edge, vivaldi)")
	addr := flag.String("addr", "", "host:port to discover via HTTP /json/version instead (e.g. a relay host:port)")
	wsURL := flag.String("ws", "", "connect directly to this websocket debugger URL, skipping discovery entirely")
	swWindow := flag.Duration("sw-window", scan.DefaultServiceWorkerWindow, "how long to listen for service worker registrations")
	permissions := flag.Bool("permissions", false, "also read notification/camera/microphone permission grants (briefly switches your active tab, once per category)")
	timeout := flag.Duration("timeout", 15*time.Second, "overall timeout for the probe")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	debuggerURL := *wsURL
	var err error
	switch {
	case debuggerURL != "":
		// use as-is
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
	fmt.Printf("WS URL: %s\n\n", debuggerURL)

	client, err := cdp.Connect(ctx, debuggerURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Close()

	origins, err := scan.DiscoverOrigins(ctx, client, *swWindow)
	if err != nil {
		log.Fatalf("discover origins: %v", err)
	}

	if len(origins) == 0 {
		fmt.Println("No candidate origins found.")
		return
	}

	fmt.Printf("%d candidate origin(s):\n", len(origins))
	for _, o := range origins {
		fmt.Printf("  %-40s %v\n", o.Origin, o.Sources)
	}

	if !*permissions {
		return
	}

	for _, category := range scan.DefaultPermissionCategories {
		grants, err := scan.DiscoverPermissions(ctx, client, category)
		if err != nil {
			log.Fatalf("discover %s permissions: %v", category.Category, err)
		}
		fmt.Printf("\n%d %s permission grant(s):\n", len(grants), category.Category)
		for _, g := range grants {
			fmt.Printf("  %-40s %s\n", g.Origin, g.Status)
		}
	}
}
