package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// fakeServer is a minimal, in-process stand-in for a Chrome browser-level
// CDP websocket endpoint — used because a real Chrome instance isn't
// available in this environment. It decodes incoming JSON-RPC-shaped
// requests and hands them to a per-test handler, which returns either a
// result to reply with or an error; it can also push unsolicited events on
// demand. This exercises Client's actual request/reply matching, session
// scoping, and event dispatch against real websocket framing, rather than
// mocking Client itself.
type fakeServer struct {
	srv  *httptest.Server
	conn *websocket.Conn

	mu       sync.Mutex
	received []fakeRequest
}

type fakeRequest struct {
	ID        uint64
	Method    string
	SessionID string
	Params    json.RawMessage
}

// newFakeServer starts the server and blocks until a client has connected
// (via Connect), so callers can immediately start driving the connection.
// If handle returns skipReply true, no reply is sent at all (simulating a
// command that never completes) — without blocking the server's read loop,
// so the connection still tears down cleanly when the test ends.
func newFakeServer(t *testing.T, handle func(fakeRequest) (result any, cdpErr *cdpError, skipReply bool)) (*Client, *fakeServer) {
	t.Helper()

	fs := &fakeServer{}
	connected := make(chan struct{})

	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.SetReadLimit(maxMessageBytes)
		fs.conn = conn
		close(connected)

		ctx := context.Background()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}

			var req struct {
				ID        uint64          `json:"id"`
				Method    string          `json:"method"`
				SessionID string          `json:"sessionId"`
				Params    json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(data, &req); err != nil {
				continue
			}

			fr := fakeRequest{ID: req.ID, Method: req.Method, SessionID: req.SessionID, Params: req.Params}
			fs.mu.Lock()
			fs.received = append(fs.received, fr)
			fs.mu.Unlock()

			if handle == nil {
				continue
			}
			result, cdpErr, skipReply := handle(fr)
			if skipReply {
				continue
			}
			reply := map[string]any{"id": req.ID}
			if cdpErr != nil {
				reply["error"] = cdpErr
			} else {
				reply["result"] = result
			}
			b, _ := json.Marshal(reply)
			conn.Write(ctx, websocket.MessageText, b)
		}
	}))

	wsURL := "ws://" + strings.TrimPrefix(fs.srv.URL, "http://")
	client, err := Connect(context.Background(), wsURL)
	if err != nil {
		t.Fatalf("Connect() to fake server: %v", err)
	}
	<-connected

	t.Cleanup(func() {
		client.Close()
		fs.srv.Close()
	})

	return client, fs
}

// sendEvent pushes an unsolicited (no "id") CDP event to the client, as a
// real browser would for e.g. ServiceWorker.workerRegistrationUpdated.
func (fs *fakeServer) sendEvent(t *testing.T, method string, params any) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"method": method, "params": params})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if err := fs.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatalf("write event: %v", err)
	}
}

func TestClientCallRoundTrip(t *testing.T) {
	client, _ := newFakeServer(t, func(req fakeRequest) (any, *cdpError, bool) {
		if req.Method != "Browser.getVersion" {
			return nil, &cdpError{Code: -32601, Message: "method not found"}, false
		}
		return map[string]string{"product": "Chrome/152.0.0.0"}, nil, false
	})

	var result struct {
		Product string `json:"product"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Call(ctx, "Browser.getVersion", nil, &result); err != nil {
		t.Fatalf("Call() unexpected error: %v", err)
	}
	if result.Product != "Chrome/152.0.0.0" {
		t.Errorf("result.Product = %q, want %q", result.Product, "Chrome/152.0.0.0")
	}
}

func TestClientCallErrorReply(t *testing.T) {
	client, _ := newFakeServer(t, func(req fakeRequest) (any, *cdpError, bool) {
		return nil, &cdpError{Code: -32603, Message: "Internal error"}, false
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Call(ctx, "Storage.clearDataForOrigin", nil, nil)
	if err == nil {
		t.Fatal("Call() expected an error for an error reply, got nil")
	}
	if !strings.Contains(err.Error(), "Internal error") {
		t.Errorf("Call() error = %v, want it to mention the server's message", err)
	}
}

func TestClientCallSessionIncludesSessionID(t *testing.T) {
	var gotSessionID string
	client, _ := newFakeServer(t, func(req fakeRequest) (any, *cdpError, bool) {
		gotSessionID = req.SessionID
		return struct{}{}, nil, false
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.CallSession(ctx, "session-42", "ServiceWorker.enable", nil, nil); err != nil {
		t.Fatalf("CallSession() unexpected error: %v", err)
	}
	if gotSessionID != "session-42" {
		t.Errorf("server observed sessionId %q, want %q", gotSessionID, "session-42")
	}
}

// TestClientConcurrentCallsMatchReplies exercises the pending-request map
// under concurrency: many in-flight calls at once must each receive their
// own reply, not another goroutine's, since a real Chrome connection
// multiplexes many simultaneous commands (e.g. a batch removal) over one
// websocket. Each call sends a unique marker and the fake server echoes it
// straight back, so a misrouted reply (goroutine A getting goroutine B's
// result) is caught by the marker mismatch, not just a missing error.
func TestClientConcurrentCallsMatchReplies(t *testing.T) {
	client, _ := newFakeServer(t, func(req fakeRequest) (any, *cdpError, bool) {
		var params struct {
			Marker int `json:"marker"`
		}
		json.Unmarshal(req.Params, &params)
		return map[string]int{"marker": params.Marker}, nil, false
	})

	const n = 50
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var result struct {
				Marker int `json:"marker"`
			}
			if err := client.Call(ctx, "Test.echo", map[string]int{"marker": i}, &result); err != nil {
				errs[i] = err
				return
			}
			if result.Marker != i {
				errs[i] = fmt.Errorf("got reply for marker %d, want %d (misrouted reply)", result.Marker, i)
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: %v", i, err)
		}
	}
}

func TestClientCallContextCanceled(t *testing.T) {
	// skipReply=true: the fake server never replies, so the call can only
	// resolve via ctx — without blocking the server's read loop forever the
	// way an in-handler select{} would.
	client, _ := newFakeServer(t, func(req fakeRequest) (any, *cdpError, bool) {
		return nil, nil, true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := client.Call(ctx, "Never.responds", nil, nil)
	if err == nil {
		t.Fatal("Call() with an expiring context should return an error")
	}
}

func TestClientSubscribeReceivesEvents(t *testing.T) {
	client, fs := newFakeServer(t, nil)

	events, unsubscribe := client.Subscribe("ServiceWorker.workerRegistrationUpdated")
	defer unsubscribe()

	fs.sendEvent(t, "ServiceWorker.workerRegistrationUpdated", map[string]any{
		"registrations": []map[string]any{{"scopeURL": "https://example.com/", "isDeleted": false}},
	})

	select {
	case raw := <-events:
		var params struct {
			Registrations []struct {
				ScopeURL string `json:"scopeURL"`
			} `json:"registrations"`
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			t.Fatalf("decode event params: %v", err)
		}
		if len(params.Registrations) != 1 || params.Registrations[0].ScopeURL != "https://example.com/" {
			t.Errorf("unexpected event payload: %+v", params)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscribed event")
	}
}

func TestClientUnsubscribeStopsDelivery(t *testing.T) {
	client, fs := newFakeServer(t, nil)

	events, unsubscribe := client.Subscribe("Test.event")
	unsubscribe()

	fs.sendEvent(t, "Test.event", map[string]any{})

	select {
	case _, ok := <-events:
		if ok {
			t.Error("received an event after unsubscribing")
		}
		// A closed channel with no value is the expected post-unsubscribe state.
	case <-time.After(200 * time.Millisecond):
		// No delivery at all is also acceptable — the point is nothing came
		// through as a live event.
	}
}

func TestClientCallAfterCloseFails(t *testing.T) {
	client, _ := newFakeServer(t, func(req fakeRequest) (any, *cdpError, bool) {
		return struct{}{}, nil, false
	})
	client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Call(ctx, "Anything", nil, nil); err == nil {
		t.Error("Call() after Close() should return an error, not hang or succeed")
	}
}
