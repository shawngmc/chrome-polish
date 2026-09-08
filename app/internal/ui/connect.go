// Package ui holds the Fyne widgets that make up Chrome Polish's control
// UI (see DESIGN.md section 4).
package ui

import (
	"context"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/shawngmc/chrome-polish/app/internal/cdp"
)

const connectTimeout = 10 * time.Second

const (
	modeLocal = "This machine"
	modeAddr  = "Host:port (HTTP discovery)"
	modeWSURL = "Paste WebSocket URL"
)

// Preferences keys the connection panel persists across launches (via
// fyne.CurrentApp().Preferences(), backed by each OS's normal per-app
// settings storage). The pasted websocket URL (modeWSURL) is deliberately
// not among them: it embeds a per-launch target ID (see
// cdp-remote-debugging-quirks) that goes stale the moment Chrome restarts,
// so remembering it would just hand back a URL that no longer resolves.
const (
	prefConnectMode    = "connect.mode"
	prefConnectBrowser = "connect.browser"
	prefConnectAddr    = "connect.addr"
)

// ConnectPanel is the connection panel from DESIGN.md section 4: pick a
// local browser, a relay host:port, or paste a websocket debugger URL
// directly, then connect over CDP.
type ConnectPanel struct {
	root fyne.CanvasObject

	client *cdp.Client

	browserSelect *widget.Select
	addrEntry     *widget.Entry
	wsEntry       *widget.Entry
	modeSelect    *widget.Select
	status        *widget.Label
	connectBtn    *widget.Button
	disconnectBtn *widget.Button

	// OnConnected is called on the Fyne main thread after a successful
	// connection. OnDisconnected is called on the Fyne main thread after
	// the client disconnects, whether by user action or connection loss.
	OnConnected    func(client *cdp.Client)
	OnDisconnected func()
}

// NewConnectPanel builds a ready-to-use connection panel, restoring the
// last-used connection mode, browser, and host:port from Preferences (see
// the pref* constants above) so a returning session doesn't start from
// scratch.
func NewConnectPanel() *ConnectPanel {
	p := &ConnectPanel{}
	prefs := fyne.CurrentApp().Preferences()

	p.browserSelect = widget.NewSelect(
		[]string{string(cdp.BrowserChrome), string(cdp.BrowserBrave), string(cdp.BrowserEdge), string(cdp.BrowserVivaldi)},
		func(browser string) { prefs.SetString(prefConnectBrowser, browser) },
	)
	p.browserSelect.SetSelected(prefs.StringWithFallback(prefConnectBrowser, string(cdp.BrowserChrome)))

	p.addrEntry = widget.NewEntry()
	p.addrEntry.SetPlaceHolder("127.0.0.1:9222 or relay-host:12345")
	p.addrEntry.SetText(prefs.String(prefConnectAddr))
	p.addrEntry.OnChanged = func(addr string) { prefs.SetString(prefConnectAddr, addr) }
	p.addrEntry.Hide()

	p.wsEntry = widget.NewEntry()
	p.wsEntry.SetPlaceHolder("ws://127.0.0.1:9222/devtools/browser/...")
	p.wsEntry.Hide()

	p.modeSelect = widget.NewSelect([]string{modeLocal, modeAddr, modeWSURL}, func(mode string) {
		p.onModeChanged(mode)
		prefs.SetString(prefConnectMode, mode)
	})
	p.modeSelect.SetSelected(prefs.StringWithFallback(prefConnectMode, modeLocal))

	p.status = widget.NewLabel("Not connected.")
	p.status.Wrapping = fyne.TextWrapWord

	p.connectBtn = widget.NewButton("Connect", p.onConnect)
	p.disconnectBtn = widget.NewButton("Disconnect", p.onDisconnect)
	p.disconnectBtn.Disable()

	p.root = container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("Connect to:"), nil, p.modeSelect),
		p.browserSelect,
		p.addrEntry,
		p.wsEntry,
		container.NewGridWithColumns(2, p.connectBtn, p.disconnectBtn),
		p.status,
	)

	return p
}

// Container returns the panel's root canvas object, ready to place in a
// window's content.
func (p *ConnectPanel) Container() fyne.CanvasObject {
	return p.root
}

func (p *ConnectPanel) onModeChanged(mode string) {
	p.browserSelect.Hide()
	p.addrEntry.Hide()
	p.wsEntry.Hide()
	switch mode {
	case modeLocal:
		p.browserSelect.Show()
	case modeAddr:
		p.addrEntry.Show()
	case modeWSURL:
		p.wsEntry.Show()
	}
}

func (p *ConnectPanel) onConnect() {
	p.connectBtn.Disable()
	p.status.SetText("Connecting...")

	mode := p.modeSelect.Selected
	browser := p.browserSelect.Selected
	addr := p.addrEntry.Text
	wsURL := p.wsEntry.Text

	go p.connect(mode, browser, addr, wsURL)
}

func (p *ConnectPanel) connect(mode, browser, addr, wsURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	debuggerURL, err := resolveDebuggerURL(ctx, mode, browser, addr, wsURL)
	if err != nil {
		p.fail(err)
		return
	}

	client, err := cdp.Connect(ctx, debuggerURL)
	if err != nil {
		p.fail(err)
		return
	}

	var version struct {
		Product string `json:"product"`
	}
	if err := client.Call(ctx, "Browser.getVersion", nil, &version); err != nil {
		client.Close()
		p.fail(fmt.Errorf("connected, but Browser.getVersion failed: %w", err))
		return
	}

	p.client = client

	fyne.Do(func() {
		p.status.SetText(fmt.Sprintf("Connected to %s\n%s", version.Product, debuggerURL))
		p.disconnectBtn.Enable()
		if p.OnConnected != nil {
			p.OnConnected(client)
		}
	})
}

func resolveDebuggerURL(ctx context.Context, mode, browser, addr, wsURL string) (string, error) {
	switch mode {
	case modeLocal:
		return cdp.DiscoverLocal(cdp.Browser(browser))
	case modeAddr:
		info, err := cdp.DiscoverBrowser(ctx, addr)
		if err != nil {
			return "", err
		}
		return info.WebSocketDebuggerURL, nil
	case modeWSURL:
		if wsURL == "" {
			return "", fmt.Errorf("paste a websocket debugger URL first")
		}
		return wsURL, nil
	default:
		return "", fmt.Errorf("unknown connection mode %q", mode)
	}
}

func (p *ConnectPanel) fail(err error) {
	fyne.Do(func() {
		p.status.SetText("Failed: " + err.Error())
		p.connectBtn.Enable()
	})
}

func (p *ConnectPanel) onDisconnect() {
	if p.client != nil {
		p.client.Close()
		p.client = nil
	}
	p.status.SetText("Not connected.")
	p.connectBtn.Enable()
	p.disconnectBtn.Disable()
	if p.OnDisconnected != nil {
		p.OnDisconnected()
	}
}
