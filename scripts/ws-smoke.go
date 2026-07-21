//go:build ignore

// ws-smoke.go is a manual smoke-test client for `myagent serve`.
//
// Usage: go run scripts/ws-smoke.go ws://127.0.0.1:8765/ws?token=dev
//
// It creates a session, sends a prompt, prints every notification frame, and
// exits after session.done (or a 60s timeout).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/coder/websocket"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/ws-smoke.go <ws-url> [prompt]")
		os.Exit(2)
	}
	url := os.Args[1]
	prompt := "Say hello in exactly three words."
	if len(os.Args) > 2 {
		prompt = os.Args[2]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ws.SetReadLimit(16 * 1024 * 1024)

	send := func(id int, method string, params any) {
		frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if err := ws.Write(ctx, websocket.MessageText, frame); err != nil {
			fmt.Fprintln(os.Stderr, "write:", err)
			os.Exit(1)
		}
	}

	var sessionID string
	send(1, "session.create", map[string]any{})

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read:", err)
			os.Exit(1)
		}
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		switch {
		case string(frame.ID) == "1": // session.create reply
			if frame.Error != nil {
				fmt.Println("create error:", string(frame.Error))
				os.Exit(1)
			}
			var res struct {
				SessionID string `json:"sessionId"`
				Model     string `json:"model"`
			}
			_ = json.Unmarshal(frame.Result, &res)
			sessionID = res.SessionID
			fmt.Printf("created session %s (model %s)\n", res.SessionID, res.Model)
			send(2, "session.prompt", map[string]any{"sessionId": sessionID, "message": prompt})
		case string(frame.ID) == "2": // prompt ack
			fmt.Println("prompt ack:", string(frame.Result), string(frame.Error))
		case frame.Method == "session.event":
			var p struct {
				Event struct {
					Type                  string `json:"type"`
					AssistantMessageEvent *struct {
						Type  string `json:"type"`
						Delta string `json:"delta"`
					} `json:"assistantMessageEvent"`
				} `json:"event"`
			}
			_ = json.Unmarshal(frame.Params, &p)
			if p.Event.Type == "message_update" && p.Event.AssistantMessageEvent != nil && p.Event.AssistantMessageEvent.Type == "text_delta" {
				fmt.Print(p.Event.AssistantMessageEvent.Delta)
			} else {
				fmt.Println("[event]", p.Event.Type)
			}
		case frame.Method == "session.done":
			fmt.Println("\n[done]", string(frame.Params))
			return
		default:
			fmt.Println("[frame]", string(data))
		}
	}
}
