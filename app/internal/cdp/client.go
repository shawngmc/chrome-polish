// Package cdp is a minimal, transport-agnostic Chrome DevTools Protocol
// client: connect to a browser-level websocket endpoint (obtained via
// DiscoverBrowser) and issue JSON-RPC-style commands.
package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"nhooyr.io/websocket"
)

// maxMessageBytes overrides the websocket library's default 32KiB
// per-message read limit. CDP responses can be large (a busy profile's
// cookie jar, DOM dumps, etc.).
const maxMessageBytes = 256 * 1024 * 1024

// Client is a connection to a single browser-level CDP endpoint.
type Client struct {
	conn   *websocket.Conn
	nextID atomic.Uint64

	mu      sync.Mutex
	pending map[uint64]chan cdpReply
	subs    map[string][]chan json.RawMessage

	closed chan struct{}
}

type cdpReply struct {
	result json.RawMessage
	err    error
}

type cdpRequest struct {
	ID        uint64 `json:"id"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

type cdpMessage struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *cdpError       `json:"error"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string {
	return fmt.Sprintf("cdp: %s (code %d)", e.Message, e.Code)
}

// Connect dials a browser-level CDP websocket endpoint, as returned by
// BrowserInfo.WebSocketDebuggerURL.
func Connect(ctx context.Context, webSocketDebuggerURL string) (*Client, error) {
	conn, _, err := websocket.Dial(ctx, webSocketDebuggerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cdp: dial %s: %w", webSocketDebuggerURL, err)
	}
	// CDP responses (e.g. Storage.getCookies on a real profile, or DOM
	// dumps) routinely exceed the library's 32KiB default.
	conn.SetReadLimit(maxMessageBytes)

	c := &Client{
		conn:    conn,
		pending: make(map[uint64]chan cdpReply),
		subs:    make(map[string][]chan json.RawMessage),
		closed:  make(chan struct{}),
	}
	go c.readLoop()

	return c, nil
}

func (c *Client) readLoop() {
	defer close(c.closed)
	for {
		_, data, err := c.conn.Read(context.Background())
		if err != nil {
			c.failAllPending(fmt.Errorf("cdp: connection closed: %w", err))
			return
		}

		var msg cdpMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		if msg.ID == 0 {
			if msg.Method != "" {
				c.dispatchEvent(msg.Method, msg.Params)
			}
			continue
		}

		c.mu.Lock()
		ch, ok := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		if !ok {
			continue
		}

		if msg.Error != nil {
			ch <- cdpReply{err: msg.Error}
		} else {
			ch <- cdpReply{result: msg.Result}
		}
		close(ch)
	}
}

func (c *Client) failAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		ch <- cdpReply{err: err}
		close(ch)
		delete(c.pending, id)
	}
	for method, chans := range c.subs {
		for _, ch := range chans {
			close(ch)
		}
		delete(c.subs, method)
	}
}

func (c *Client) dispatchEvent(method string, params json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.subs[method] {
		select {
		case ch <- params:
		default:
			// Subscriber isn't keeping up; drop the event rather than
			// block the read loop.
		}
	}
}

// Subscribe returns a channel of raw event params for the given CDP event
// method (e.g. "ServiceWorker.workerRegistrationUpdated"), and an
// unsubscribe function that must be called to release it.
func (c *Client) Subscribe(method string) (events <-chan json.RawMessage, unsubscribe func()) {
	ch := make(chan json.RawMessage, 32)

	c.mu.Lock()
	c.subs[method] = append(c.subs[method], ch)
	c.mu.Unlock()

	unsubscribe = func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		chans := c.subs[method]
		for i, existing := range chans {
			if existing == ch {
				c.subs[method] = append(chans[:i], chans[i+1:]...)
				close(ch)
				return
			}
		}
	}

	return ch, unsubscribe
}

// Call issues a browser-level CDP command and waits for its reply, decoding
// the result into result (which may be nil if the caller doesn't need the
// payload).
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	return c.call(ctx, "", method, params, result)
}

// CallSession is like Call, but scopes the command to a target session
// obtained via Target.attachToTarget (with flatten: true). Some CDP
// domains — ServiceWorker among them — are only available within a target
// session, not on the browser-level connection itself.
func (c *Client) CallSession(ctx context.Context, sessionID, method string, params any, result any) error {
	return c.call(ctx, sessionID, method, params, result)
}

func (c *Client) call(ctx context.Context, sessionID, method string, params any, result any) error {
	id := c.nextID.Add(1)

	data, err := json.Marshal(cdpRequest{ID: id, Method: method, Params: params, SessionID: sessionID})
	if err != nil {
		return fmt.Errorf("cdp: marshal %s request: %w", method, err)
	}

	ch := make(chan cdpReply, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("cdp: send %s: %w", method, err)
	}

	select {
	case reply := <-ch:
		if reply.err != nil {
			return fmt.Errorf("cdp: %s: %w", method, reply.err)
		}
		if result != nil && reply.result != nil {
			if err := json.Unmarshal(reply.result, result); err != nil {
				return fmt.Errorf("cdp: decode %s result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return fmt.Errorf("cdp: %s: connection closed", method)
	}
}

// Close closes the underlying websocket connection.
func (c *Client) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "")
}
