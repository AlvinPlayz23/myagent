// Package ws implements the WebSocket transport for myagent's server mode.
// Each text frame carries exactly one JSON-RPC 2.0 message (see internal/
// server/rpc); agent events stream to the client as `session.event`
// notifications and run completion as `session.done`.
package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/myagent/myagent/internal/server/core"
)

// Options configures the WebSocket server.
type Options struct {
	// Addr is the listen address, e.g. "127.0.0.1:8765".
	Addr string
	// OnListen is called after the TCP listener has been created.
	OnListen func(net.Addr)
	// Token is the shared bearer token clients must present at upgrade time
	// (via `?token=` or `Authorization: Bearer`). Required.
	Token string
	// Version is reported to clients in the server.hello notification.
	Version string
}

// NewToken generates a random hex token for --token-less startups.
func NewToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return hex.EncodeToString(b[:])
}

// Serve runs the WebSocket server until ctx is canceled. It returns after the
// listener is closed and in-flight connections have been told to shut down.
func Serve(ctx context.Context, manager *core.Manager, opts Options) error {
	if opts.Token == "" {
		return errors.New("ws: a token is required")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(ctx, w, r, manager, opts)
	})

	srv := &http.Server{
		Addr:    opts.Addr,
		Handler: mux,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return err
	}
	if opts.OnListen != nil {
		opts.OnListen(ln.Addr())
	}
	log.Printf("myagent server listening on ws://%s/ws", ln.Addr())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// serveWS authenticates and upgrades one connection, then runs its read loop.
func serveWS(ctx context.Context, w http.ResponseWriter, r *http.Request, manager *core.Manager, opts Options) {
	if !authorized(r, opts.Token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Reject browser-origin connections outright: WebSockets bypass CORS, so
	// any web page could otherwise drive a local agent. First-party clients
	// (desktop apps, CLIs) send no Origin header.
	if origin := r.Header.Get("Origin"); origin != "" {
		http.Error(w, "browser origins are not allowed", http.StatusForbidden)
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Origin is rejected above; skip the library's same-host check, which
		// would also block non-browser clients that happen to set Origin.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	conn := newConn(ctx, c, manager, opts.Version)
	conn.run()
}

// authorized checks the shared token in the query string or the
// Authorization header.
func authorized(r *http.Request, token string) bool {
	if q := r.URL.Query().Get("token"); q != "" {
		return q == token
	}
	if h := r.Header.Get("Authorization"); h != "" {
		return strings.TrimPrefix(h, "Bearer ") == token
	}
	return false
}

// newConnID generates a unique id for a connection (session ownership key).
func newConnID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("conn-%d", time.Now().UnixNano())
	}
	return "conn-" + hex.EncodeToString(b[:])
}
