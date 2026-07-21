package ws

import (
	"context"
	"sync"

	"github.com/coder/websocket"

	"github.com/myagent/myagent/internal/server/core"
	"github.com/myagent/myagent/internal/server/rpc"
	"github.com/myagent/myagent/internal/types"
)

// conn is one live WebSocket connection. A read loop parses JSON-RPC frames
// and dispatches each request on its own goroutine (so long-running handlers
// never block reads); writes are serialized by writeMu per coder/websocket's
// single-writer requirement. Each session the connection owns gets an event
// pump goroutine forwarding session.event / session.done notifications.
type conn struct {
	id      string
	ws      *websocket.Conn
	manager *core.Manager
	version string

	// ctx is canceled when the connection closes (or the server shuts down),
	// stopping event pumps and in-flight handler writes.
	ctx    context.Context
	cancel context.CancelFunc

	writeMu sync.Mutex

	pumpMu sync.Mutex
	pumps  map[string]struct{} // session ids with an active event pump
	wg     sync.WaitGroup
}

func newConn(parent context.Context, ws *websocket.Conn, manager *core.Manager, version string) *conn {
	ctx, cancel := context.WithCancel(parent)
	return &conn{
		id:      newConnID(),
		ws:      ws,
		manager: manager,
		version: version,
		ctx:     ctx,
		cancel:  cancel,
		pumps:   map[string]struct{}{},
	}
}

// run drives the connection until the peer disconnects or the server shuts
// down. On exit every owned session's run is aborted and ownership released
// (sessions stay resumable).
func (c *conn) run() {
	defer func() {
		c.cancel()
		c.manager.ReleaseOwner(c.id)
		c.wg.Wait()
		_ = c.ws.Close(websocket.StatusNormalClosure, "")
	}()

	c.notify("server.hello", map[string]any{
		"name":     "myagent",
		"version":  c.version,
		"protocol": 1,
	})

	for {
		typ, data, err := c.ws.Read(c.ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		req, rpcErr := rpc.Parse(data)
		if rpcErr != nil {
			c.writeError(nil, rpcErr)
			continue
		}
		// Dispatch on a fresh goroutine so a slow handler (e.g. one that
		// resolves a provider) never stalls the read loop; per-session state
		// is guarded inside core.
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.dispatch(req)
		}()
	}
}

// write sends one frame, serialized against concurrent writers.
func (c *conn) write(data []byte) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.Write(c.ctx, websocket.MessageText, data)
}

// notify sends a server→client notification.
func (c *conn) notify(method string, params any) {
	data, err := rpc.MarshalNotification(method, params)
	if err != nil {
		return
	}
	c.write(data)
}

// writeResult replies to a request with a result.
func (c *conn) writeResult(id []byte, result any) {
	data, err := rpc.MarshalResult(id, result)
	if err != nil {
		return
	}
	c.write(data)
}

// writeError replies to a request with an error.
func (c *conn) writeError(id []byte, rpcErr *rpc.Error) {
	data, err := rpc.MarshalError(id, rpcErr)
	if err != nil {
		return
	}
	c.write(data)
}

// startEventPump forwards a session's events to the client as notifications.
// Idempotent per session: resuming an already-pumped session does not start a
// second pump.
func (c *conn) startEventPump(ss *core.ServerSession) {
	c.pumpMu.Lock()
	if _, ok := c.pumps[ss.ID()]; ok {
		c.pumpMu.Unlock()
		return
	}
	c.pumps[ss.ID()] = struct{}{}
	c.pumpMu.Unlock()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			c.pumpMu.Lock()
			delete(c.pumps, ss.ID())
			c.pumpMu.Unlock()
		}()
		for {
			select {
			case ev := <-ss.Events():
				if ev.Done {
					params := map[string]any{"sessionId": ev.SessionID}
					if ev.Err != nil {
						params["error"] = ev.Err.Error()
					}
					c.notify("session.done", params)
					continue
				}
				if ev.AgentEvent != nil {
					c.notify("session.event", sessionEventParams{
						SessionID: ev.SessionID,
						Event:     ev.AgentEvent,
					})
				}
			case <-c.ctx.Done():
				return
			}
		}
	}()
}

// sessionEventParams is the payload of a session.event notification: the
// AgentEvent serialized verbatim alongside its session id.
type sessionEventParams struct {
	SessionID string            `json:"sessionId"`
	Event     *types.AgentEvent `json:"event"`
}
