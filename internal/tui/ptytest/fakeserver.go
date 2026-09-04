//go:build unix

package ptytest

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ProviderName and ModelID are the custom provider identity the harness writes
// into the temporary config.json, and the model list the fake server serves.
const (
	ProviderName = "ptyfake"
	ModelID      = "ptyfake-stream"
	ModelRef     = ProviderName + "/" + ModelID
	titleReply   = "Fake Session Title"
)

// defaultScript answers agent requests that have no queued script left.
var defaultScript = Script{Text: []string{"Default scripted reply from the fake server."}}

// Script describes one scripted streaming chat completion served to an agent
// (non-title) request. Chunk shapes mirror internal/llm's chatChunk parsing:
// reasoning deltas arrive as delta.reasoning_content before text deltas, and
// the stream must end with a finish_reason chunk so the provider's guard
// checks pass.
type Script struct {
	Thinking     []string // reasoning deltas, emitted before text deltas
	Text         []string // assistant text deltas
	FinishReason string   // defaults to "stop"
	// GateAfter parks the stream after this many deltas (thinking and text
	// counted together) until Release is called, giving tests a deterministic
	// mid-stream window instead of a timing race. Zero disables gating.
	GateAfter int
}

// Request is one captured chat-completions request, for debugging and
// protocol assertions.
type Request struct {
	Model    string
	Body     string
	TitleGen bool
}

// Server is a fake OpenAI-compatible provider listening on an ephemeral
// loopback address. It implements the two endpoints myagent uses:
// POST {base}/v1/chat/completions with SSE streaming and GET {base}/v1/models.
type Server struct {
	mu       sync.Mutex
	srv      *http.Server
	base     string
	scripts  []Script
	gate     chan struct{}
	gateOpen bool
	// released remembers an early Release that arrived before a gated stream
	// created its gate, so the next gate starts already open.
	released bool
	reqs     []Request
	calls    atomic.Int64
}

// NewServer starts the fake provider on 127.0.0.1:0. Callers must Close it.
func NewServer() (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{}
	mux := http.NewServeMux()
	// The configured baseUrl ends in /v1; the un-prefixed routes tolerate
	// configurations that omit it.
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChat)
	mux.HandleFunc("/chat/completions", s.handleChat)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	s.base = "http://" + ln.Addr().String()
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

// BaseURL returns the fake provider's base address (without /v1).
func (s *Server) BaseURL() string { return s.base }

// EnqueueScript appends a script; agent requests consume scripts FIFO.
func (s *Server) EnqueueScript(sc Script) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts = append(s.scripts, sc)
}

// Release unblocks a stream parked at its GateAfter gate. It is safe to call
// before the gate exists (the gate is then created released) or repeatedly.
func (s *Server) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate != nil && s.gateOpen {
		close(s.gate)
		s.gateOpen = false
		return
	}
	s.released = true
}

// Requests returns a copy of every captured chat request, in order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// Close shuts the server down.
func (s *Server) Close() error { return s.srv.Close() }

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"object":"list","data":[{"id":%q,"object":"model","owned_by":%q}]}`, ModelID, ProviderName)
}

// chatRequest mirrors only the request fields the fake server needs to route a
// call. Field names match internal/llm's request encoding.
type chatRequest struct {
	Model     string            `json:"model"`
	Messages  []chatReqMessage  `json:"messages"`
	Tools     []json.RawMessage `json:"tools"`
	MaxTokens *int              `json:"max_completion_tokens"`
}

type chatReqMessage struct {
	Role string `json:"role"`
}

// isTitleRequest detects myagent's isolated title-generation call: it is the
// only request sent without tools and with a small max_completion_tokens.
func isTitleRequest(req chatRequest) bool {
	return len(req.Tools) == 0 && req.MaxTokens != nil
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var req chatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, Request{Model: req.Model, Body: string(body), TitleGen: isTitleRequest(req)})
	s.mu.Unlock()

	if isTitleRequest(req) {
		s.stream(w, r, Script{Text: []string{titleReply}})
		return
	}
	s.stream(w, r, s.nextScript())
}

func (s *Server) nextScript() Script {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.scripts) == 0 {
		return defaultScript
	}
	sc := s.scripts[0]
	s.scripts = s.scripts[1:]
	return sc
}

func (s *Server) newGate() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gate = make(chan struct{})
	if s.released {
		s.released = false
		close(s.gate)
	} else {
		s.gateOpen = true
	}
	return s.gate
}

// stream writes one SSE completion shaped exactly like the chunk stream
// internal/llm/openai.go parses: data-prefixed JSON chunks, an explicit
// finish_reason chunk, a usage chunk, then [DONE].
func (s *Server) stream(w http.ResponseWriter, r *http.Request, sc Script) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	emit := func(payload any) {
		b, err := json.Marshal(payload)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		flush()
	}

	id := fmt.Sprintf("chatcmpl-ptytest-%d", s.calls.Add(1))
	finish := sc.FinishReason
	if finish == "" {
		finish = "stop"
	}

	type delta struct {
		thinking bool
		text     string
	}
	var deltas []delta
	for _, d := range sc.Thinking {
		deltas = append(deltas, delta{thinking: true, text: d})
	}
	for _, d := range sc.Text {
		deltas = append(deltas, delta{thinking: false, text: d})
	}

	var gate chan struct{}
	if sc.GateAfter > 0 && sc.GateAfter < len(deltas) {
		gate = s.newGate()
	}

	for i, d := range deltas {
		if gate != nil && i == sc.GateAfter {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		if d.thinking {
			emit(chunk(id, fakeDelta{ReasoningContent: d.text}, nil))
		} else {
			emit(chunk(id, fakeDelta{Content: d.text}, nil))
		}
	}
	stop := finish
	emit(chunk(id, fakeDelta{}, &stop))
	emit(fakeChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Model:   ModelID,
		Choices: []fakeChoice{},
		Usage: &fakeUsage{
			PromptTokens:     64,
			CompletionTokens: 32,
			CompletionTokensDetails: struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			}{ReasoningTokens: 16},
		},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush()
}

func chunk(id string, d fakeDelta, finish *string) fakeChunk {
	return fakeChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Model:   ModelID,
		Choices: []fakeChoice{{Index: 0, Delta: d, FinishReason: finish}},
	}
}

// fakeDelta, fakeChoice, fakeChunk, and fakeUsage mirror the wire shapes
// internal/llm decodes (chatDelta, chatChoice, chatChunk, chunkUsage).
type fakeDelta struct {
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type fakeChoice struct {
	Index        int       `json:"index"`
	Delta        fakeDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type fakeChunk struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Model   string       `json:"model"`
	Choices []fakeChoice `json:"choices"`
	Usage   *fakeUsage   `json:"usage,omitempty"`
}

type fakeUsage struct {
	PromptTokens            int `json:"prompt_tokens"`
	CompletionTokens        int `json:"completion_tokens"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}
