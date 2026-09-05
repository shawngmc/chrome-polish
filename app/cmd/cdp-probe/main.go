// Command cdp-probe is a diagnostic tool: connect to a Chrome/Chromium
// instance's browser-level CDP endpoint and confirm the round trip works,
// without any of the scanning/removal logic. Useful for verifying that
// remote debugging is reachable before the real tool needs it.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

func main() {
	browser := flag.String("browser", "chrome", "local browser to auto-discover via its DevToolsActivePort file (chrome, brave, edge, vivaldi)")
	addr := flag.String("addr", "", "host:port to discover via HTTP /json/version instead (e.g. a relay host:port, or a browser launched with --remote-debugging-port)")
	wsURL := flag.String("ws", "", "connect directly to this websocket debugger URL, skipping discovery entirely")
	timeout := flag.Duration("timeout", 10*time.Second, "overall timeout for the probe")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	debuggerURL := *wsURL
	switch {
	case debuggerURL != "":
		// use as-is

	case *addr != "":
		info, err := cdp.DiscoverBrowser(ctx, *addr)
		if err != nil {
			log.Fatalf("discover %s: %v", *addr, err)
		}
		fmt.Printf("Browser:  %s\n", info.Browser)
		fmt.Printf("Protocol: %s\n", info.ProtocolVersion)
		fmt.Printf("WS URL:   %s\n", info.WebSocketDebuggerURL)
		debuggerURL = info.WebSocketDebuggerURL

	default:
		url, err := cdp.DiscoverLocal(cdp.Browser(*browser))
		if err != nil {
			log.Fatalf("discover local %s: %v", *browser, err)
		}
		fmt.Printf("WS URL:   %s\n", url)
		debuggerURL = url
	}

	client, err := cdp.Connect(ctx, debuggerURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Close()

	var version struct {
		Product         string `json:"product"`
		Revision        string `json:"revision"`
		UserAgent       string `json:"userAgent"`
		JSVersion       string `json:"jsVersion"`
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := client.Call(ctx, "Browser.getVersion", nil, &version); err != nil {
		log.Fatalf("Browser.getVersion: %v", err)
	}

	fmt.Println("\nBrowser.getVersion:")
	fmt.Printf("  product:  %s\n", version.Product)
	fmt.Printf("  revision: %s\n", version.Revision)
	fmt.Printf("  jsVersion: %s\n", version.JSVersion)
}
