package ws

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/server/core"
	"github.com/myagent/myagent/internal/server/rpc"
	"github.com/myagent/myagent/internal/types"
)

// fakeProvider replies to every request with canned text.
type fakeProvider struct {
	mu       sync.Mutex
	requests []llm.Request
	reply    string
}

func (p *fakeProvider) Stream(ctx context.Context, model llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	out := make(chan llm.StreamEvent, 4)
	go func() {
		defer close(out)
		out <- llm.StreamEvent{Type: "start", Partial: &types.Message{Role: types.RoleAssistant}}
		out <- llm.StreamEvent{Type: "text_delta", Delta: p.reply}
		out <- llm.StreamEvent{Type: "done", Message: &types.Message{
			Role:       types.RoleAssistant,
			Content:    []types.ContentBlock{types.TextBlock(p.reply)},
			StopReason: types.StopStop,
		}}
	}()
	return out, nil
}

// testServer wires a Manager + WS handler behind httptest.
func testServer(t *testing.T, provider llm.Provider) (url string, token string) {
	t.Helper()
	t.Setenv("MYAGENT_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	manager := core.NewManager(ctx, core.Options{
		Resolve: func(providerName, modelID string) (llm.Provider, llm.Model, error) {
			if modelID == "" {
				modelID = "test-model"
			}
			return provider, llm.Model{ID: modelID, Provider: "test", BaseURL: "http://unused"}, nil
		},
		DefaultCwd: t.TempDir(),
	})
	token = "test-token"
	opts := Options{Token: token, Version: "test"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveWS(ctx, w, r, manager, opts)
	}))
	t.Cleanup(func() {
		srv.Close()
		cancel()
		manager.Shutdown()
	})
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=" + token, token
}

func TestServeCallsOnListenAfterBinding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var callbackErr error
	err := Serve(ctx, nil, Options{
		Addr:  "127.0.0.1:0",
		Token: "test-token",
		OnListen: func(addr net.Addr) {
			conn, err := net.DialTimeout("tcp", addr.String(), time.Second)
			if err != nil {
				callbackErr = err
			} else {
				_ = conn.Close()
			}
			cancel()
		},
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if callbackErr != nil {
		t.Fatalf("listener was not reachable during OnListen: %v", callbackErr)
	}
}

// client is a minimal test JSON-RPC websocket client.
type client struct {
	t    *testing.T
	ws   *websocket.Conn
	ctx  context.Context
	mu   sync.Mutex
	next int

	// incoming frames are demultiplexed into responses (by id) and
	// notifications (in order).
	respMu    sync.Mutex
	responses map[string]chan rpc.Response
	notifs    chan rpc.Request // reusing Request shape for method+params
}

func dial(t *testing.T, url string) *client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		cancel()
		t.Fatalf("dial: %v", err)
	}
	c := &client{
		t:         t,
		ws:        ws,
		ctx:       ctx,
		responses: map[string]chan rpc.Response{},
		notifs:    make(chan rpc.Request, 256),
	}
	go c.readLoop()
	t.Cleanup(func() {
		_ = ws.Close(websocket.StatusNormalClosure, "")
		cancel()
	})
	return c
}

func (c *client) readLoop() {
	for {
		_, data, err := c.ws.Read(c.ctx)
		if err != nil {
			return
		}
		var probe struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			continue
		}
		if probe.Method != "" {
			var n rpc.Request
			_ = json.Unmarshal(data, &n)
			select {
			case c.notifs <- n:
			default:
			}
			continue
		}
		var resp rpc.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}
		c.respMu.Lock()
		ch := c.responses[string(resp.ID)]
		c.respMu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
}

// call sends a request and waits for its response.
func (c *client) call(method string, params any) rpc.Response {
	c.t.Helper()
	c.mu.Lock()
	c.next++
	id := c.next
	c.mu.Unlock()

	idJSON, _ := json.Marshal(id)
	ch := make(chan rpc.Response, 1)
	c.respMu.Lock()
	c.responses[string(idJSON)] = ch
	c.respMu.Unlock()

	var paramsJSON json.RawMessage
	if params != nil {
		paramsJSON, _ = json.Marshal(params)
	}
	frame, _ := json.Marshal(rpc.Request{JSONRPC: rpc.Version, ID: idJSON, Method: method, Params: paramsJSON})
	if err := c.ws.Write(c.ctx, websocket.MessageText, frame); err != nil {
		c.t.Fatalf("write: %v", err)
	}

	select {
	case resp := <-ch:
		return resp
	case <-time.After(10 * time.Second):
		c.t.Fatalf("timed out waiting for response to %s", method)
		return rpc.Response{}
	}
}

// result decodes a successful response's result into dst.
func (c *client) result(resp rpc.Response, dst any) {
	c.t.Helper()
	if resp.Error != nil {
		c.t.Fatalf("rpc error: %v", resp.Error)
	}
	b, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(b, dst); err != nil {
		c.t.Fatalf("decode result: %v", err)
	}
}

// waitNotif waits for the next notification with the given method, skipping
// others.
func (c *client) waitNotif(method string) rpc.Request {
	c.t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case n := <-c.notifs:
			if n.Method == method {
				return n
			}
		case <-timeout:
			c.t.Fatalf("timed out waiting for %s notification", method)
		}
	}
}

func TestEndToEndPromptFlow(t *testing.T) {
	url, _ := testServer(t, &fakeProvider{reply: "hi from server"})
	c := dial(t, url)

	// server.hello arrives on connect.
	hello := c.waitNotif("server.hello")
	var helloParams struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	}
	_ = json.Unmarshal(hello.Params, &helloParams)
	if helloParams.Name != "myagent" || helloParams.Protocol != 1 {
		t.Errorf("hello = %+v", helloParams)
	}

	// Create a session.
	var created struct {
		SessionID string `json:"sessionId"`
		Model     string `json:"model"`
	}
	c.result(c.call("session.create", nil), &created)
	if created.SessionID == "" {
		t.Fatal("empty sessionId")
	}
	if created.Model != "test/test-model" {
		t.Errorf("model = %q", created.Model)
	}

	// Prompt and stream.
	c.result(c.call("session.prompt", map[string]any{"sessionId": created.SessionID, "message": "hello"}), &struct{}{})

	// Expect agent events then session.done.
	var sawAgentEnd bool
	for !sawAgentEnd {
		n := c.waitNotif("session.event")
		var p struct {
			SessionID string           `json:"sessionId"`
			Event     types.AgentEvent `json:"event"`
		}
		if err := json.Unmarshal(n.Params, &p); err != nil {
			t.Fatal(err)
		}
		if p.SessionID != created.SessionID {
			t.Errorf("event for session %q, want %q", p.SessionID, created.SessionID)
		}
		if p.Event.Type == types.EventAgentEnd {
			sawAgentEnd = true
		}
	}
	done := c.waitNotif("session.done")
	var doneParams struct {
		SessionID string `json:"sessionId"`
		Error     string `json:"error"`
	}
	_ = json.Unmarshal(done.Params, &doneParams)
	if doneParams.Error != "" {
		t.Errorf("done error = %q", doneParams.Error)
	}

	// History is queryable.
	var msgs struct {
		Messages []types.Message `json:"messages"`
	}
	c.result(c.call("session.messages", map[string]any{"sessionId": created.SessionID}), &msgs)
	if len(msgs.Messages) != 2 {
		t.Errorf("messages = %d, want 2", len(msgs.Messages))
	}
}

func TestErrorMapping(t *testing.T) {
	url, _ := testServer(t, &fakeProvider{reply: "x"})
	c := dial(t, url)

	// Unknown method.
	if resp := c.call("nope.nope", nil); resp.Error == nil || resp.Error.Code != rpc.CodeMethodNotFound {
		t.Errorf("unknown method error = %+v", resp.Error)
	}
	// Unknown session.
	if resp := c.call("session.prompt", map[string]any{"sessionId": "missing", "message": "x"}); resp.Error == nil || resp.Error.Code != rpc.CodeSessionNotFound {
		t.Errorf("missing session error = %+v", resp.Error)
	}
	// Missing params.
	if resp := c.call("session.prompt", map[string]any{}); resp.Error == nil || resp.Error.Code != rpc.CodeInvalidParams {
		t.Errorf("missing params error = %+v", resp.Error)
	}
	// Steer while idle.
	var created struct {
		SessionID string `json:"sessionId"`
	}
	c.result(c.call("session.create", nil), &created)
	if resp := c.call("session.steer", map[string]any{"sessionId": created.SessionID, "message": "x"}); resp.Error == nil || resp.Error.Code != rpc.CodeSessionNotRunning {
		t.Errorf("steer idle error = %+v", resp.Error)
	}
}

func TestAuthRejection(t *testing.T) {
	url, _ := testServer(t, &fakeProvider{reply: "x"})

	// Wrong token → dial fails with 401.
	badURL := strings.Split(url, "?")[0] + "?token=wrong"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, resp, err := websocket.Dial(ctx, badURL, nil); err == nil {
		t.Error("dial with wrong token succeeded")
	} else if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}

	// Browser Origin → rejected.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if _, resp, err := websocket.Dial(ctx2, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}},
	}); err == nil {
		t.Error("dial with browser origin succeeded")
	} else if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestSessionListAndResume(t *testing.T) {
	url, _ := testServer(t, &fakeProvider{reply: "persisted"})
	c := dial(t, url)

	var created struct {
		SessionID string `json:"sessionId"`
	}
	c.result(c.call("session.create", nil), &created)
	c.result(c.call("session.prompt", map[string]any{"sessionId": created.SessionID, "message": "hello"}), &struct{}{})
	c.waitNotif("session.done")
	c.result(c.call("session.close", map[string]any{"sessionId": created.SessionID}), &struct{}{})

	// List shows the persisted session.
	var list struct {
		Sessions []struct {
			ID           string `json:"id"`
			MessageCount int    `json:"messageCount"`
		} `json:"sessions"`
	}
	c.result(c.call("session.list", nil), &list)
	if len(list.Sessions) != 1 || list.Sessions[0].ID != created.SessionID {
		t.Fatalf("list = %+v", list.Sessions)
	}
	if list.Sessions[0].MessageCount != 2 {
		t.Errorf("messageCount = %d, want 2", list.Sessions[0].MessageCount)
	}

	// Resume restores history.
	var resumed struct {
		SessionID string          `json:"sessionId"`
		Messages  []types.Message `json:"messages"`
	}
	c.result(c.call("session.resume", map[string]any{"sessionId": created.SessionID}), &resumed)
	if len(resumed.Messages) != 2 {
		t.Errorf("resumed messages = %d, want 2", len(resumed.Messages))
	}
}

func TestTwoClientsOwnership(t *testing.T) {
	url, _ := testServer(t, &fakeProvider{reply: "x"})
	c1 := dial(t, url)
	c2 := dial(t, url)

	var created struct {
		SessionID string `json:"sessionId"`
	}
	c1.result(c1.call("session.create", nil), &created)

	// Second client cannot act on the first client's session.
	if resp := c2.call("session.prompt", map[string]any{"sessionId": created.SessionID, "message": "steal"}); resp.Error == nil || resp.Error.Code != rpc.CodeNotOwner {
		t.Errorf("cross-client prompt error = %+v", resp.Error)
	}
}
