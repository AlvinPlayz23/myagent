package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestIsMuseSparkModel(t *testing.T) {
	for _, id := range []string{"muse-spark-1.3", "muse-spark-1.3-contributor-free", "MUSE-SPARK-1.2"} {
		if !IsMuseSparkModel(id) {
			t.Errorf("IsMuseSparkModel(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"gpt-5.5", "minimax-m2.7", "", "muse-image"} {
		if IsMuseSparkModel(id) {
			t.Errorf("IsMuseSparkModel(%q) = true, want false", id)
		}
	}
}

func TestClampContributorEffort(t *testing.T) {
	if got := ClampContributorEffort("muse-spark-1.3-contributor-free", EffortMax); got != EffortXHigh {
		t.Fatalf("clamp max contributor = %q, want xhigh", got)
	}
	if got := ClampContributorEffort("muse-spark-1.3", EffortMax); got != EffortMax {
		t.Fatalf("clamp max standard = %q, want max", got)
	}
	if got := ClampContributorEffort("muse-spark-1.3-contributor", EffortHigh); got != EffortHigh {
		t.Fatalf("clamp high contributor = %q, want high", got)
	}
	if got := ClampContributorEffort("gpt-5.5", EffortMax); got != EffortMax {
		t.Fatalf("clamp max other = %q, want max", got)
	}
}

func TestClassifyZenError(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		model    string
		endpoint string
		wantSub  string
		wantNone bool
	}{
		{
			name:    "free tier gated",
			status:  400,
			body:    `{"type":"error","error":{"type":"MissingSessionID","message":"Error from provider (Console): OpenCode's free tier can only be used in OpenCode"}}`,
			model:   "test-model-free",
			wantSub: "requires an OpenCode client session",
		},
		{
			name:    "model unavailable",
			status:  400,
			body:    `{"error":{"type":"server_error","message":"Error from provider (Console): Upstream request failed: Model is unavailable."}}`,
			model:   "some-model",
			wantSub: "unavailable/unsupported",
		},
		{
			name:    "model not supported",
			status:  401,
			body:    `{"type":"error","error":{"type":"ModelError","message":"Model mimo-v2-flash-free is not supported"}}`,
			model:   "mimo-v2-flash-free",
			wantSub: "unavailable/unsupported",
		},
		{
			name:     "muse chat 500 hints responses",
			status:   500,
			body:     `{"type":"error","error":{"type":"error","message":"Internal server error"}}`,
			model:    "muse-spark-1.3-contributor-free",
			endpoint: "/chat/completions",
			wantSub:  "/responses",
		},
		{
			name:     "muse responses 500 has no transport hint",
			status:   500,
			body:     `{"type":"error","error":{"type":"error","message":"Internal server error"}}`,
			model:    "muse-spark-1.3-contributor-free",
			endpoint: "/responses",
			wantNone: true,
		},
		{
			name:     "unknown error no hint",
			status:   500,
			body:     `{"type":"error","message":"boom"}`,
			model:    "gpt-5.5",
			endpoint: "/chat/completions",
			wantNone: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyZenError(tc.status, tc.body, tc.model, tc.endpoint)
			if tc.wantNone {
				if got != "" {
					t.Fatalf("hint = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Fatalf("hint = %q, want substring %q", got, tc.wantSub)
			}
		})
	}
}

func TestBuildRequestBodyClampsContributorMax(t *testing.T) {
	body, err := buildRequestBody(Model{ID: "muse-spark-1.3-contributor-free"}, Request{Effort: EffortMax})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"reasoning_effort":"xhigh"`) {
		t.Fatalf("chat body = %s, want reasoning_effort xhigh", body)
	}
}

func TestBuildResponsesBody(t *testing.T) {
	maxTokens := 1024
	body, err := buildResponsesBody(
		Model{ID: "muse-spark-1.3-contributor-free"},
		Request{Effort: EffortMax, MaxTokens: &maxTokens},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{`"model":"muse-spark-1.3-contributor-free"`, `"stream":true`, `"effort":"xhigh"`, `"max_output_tokens":1024`, "reasoning.encrypted_content"} {
		if !strings.Contains(s, want) {
			t.Fatalf("responses body missing %s: %s", want, s)
		}
	}
}

func TestIsZenHost(t *testing.T) {
	for _, base := range []string{
		"https://opencode.ai/zen/v1",
		"https://opencode.ai/zen/v1/",
		"http://opencode.ai/something",
	} {
		if !IsZenHost(base) {
			t.Errorf("IsZenHost(%q) = false, want true", base)
		}
	}
	for _, base := range []string{
		"https://api.openai.com/v1",
		"https://zenmux.ai/api/v1",
		"http://localhost:11434/v1",
		"",
		"::://bad",
	} {
		if IsZenHost(base) {
			t.Errorf("IsZenHost(%q) = true, want false", base)
		}
	}
}

func TestSetZenHeaders(t *testing.T) {
	newReq := func() *http.Request {
		r, err := http.NewRequest(http.MethodPost, "https://opencode.ai/zen/v1/responses", nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// Zen host + session: all three headers set.
	r := newReq()
	setZenHeaders(r, Model{BaseURL: "https://opencode.ai/zen/v1", SessionID: "sess-123"})
	if got := r.Header.Get("x-opencode-session"); got != "sess-123" {
		t.Errorf("x-opencode-session = %q, want sess-123", got)
	}
	if got := r.Header.Get("x-opencode-client"); got != "myagent" {
		t.Errorf("x-opencode-client = %q, want myagent", got)
	}
	if got := r.Header.Get("User-Agent"); got == "" {
		t.Errorf("User-Agent unset, want honest myagent UA")
	}

	// No session: nothing sent (mirrors pi: headers ride with the session ID).
	r = newReq()
	setZenHeaders(r, Model{BaseURL: "https://opencode.ai/zen/v1"})
	if got := r.Header.Get("x-opencode-session"); got != "" {
		t.Errorf("x-opencode-session = %q, want empty", got)
	}
	if got := r.Header.Get("x-opencode-client"); got != "" {
		t.Errorf("x-opencode-client = %q, want empty", got)
	}

	// Non-Zen host: never sent, even with a session (no leaking).
	r = newReq()
	setZenHeaders(r, Model{BaseURL: "https://api.openai.com/v1", SessionID: "sess-123"})
	if got := r.Header.Get("x-opencode-session"); got != "" {
		t.Errorf("x-opencode-session leaked to non-Zen host: %q", got)
	}
	if got := r.Header.Get("x-opencode-client"); got != "" {
		t.Errorf("x-opencode-client leaked to non-Zen host: %q", got)
	}

	// Existing headers are preserved, not overwritten.
	r = newReq()
	r.Header.Set("x-opencode-session", "existing")
	setZenHeaders(r, Model{BaseURL: "https://opencode.ai/zen/v1", SessionID: "sess-123"})
	if got := r.Header.Get("x-opencode-session"); got != "existing" {
		t.Errorf("x-opencode-session = %q, want existing", got)
	}
}

func TestZenHeadersSentOnWire(t *testing.T) {
	ct := &captureTransport{
		status: 200,
		body: "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n",
	}
	provider := &OpenAIProvider{APIKey: "", Client: &http.Client{Transport: ct}}
	// Non-Muse ID exercises the chat-completions path.
	stream, err := provider.Stream(context.Background(),
		Model{ID: "glm-4.7-free", BaseURL: "https://opencode.ai/zen/v1", SessionID: "sess-1"},
		Request{})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, stream)
	terminal := terminalEvent(t, events)
	if terminal.Type != "done" {
		t.Fatalf("terminal = %#v, want done", terminal)
	}
	if ct.req == nil {
		t.Fatal("transport saw no request")
	}
	if got := ct.req.Header.Get("x-opencode-session"); got != "sess-1" {
		t.Errorf("x-opencode-session = %q, want sess-1", got)
	}
	if got := ct.req.Header.Get("x-opencode-client"); got != "myagent" {
		t.Errorf("x-opencode-client = %q, want myagent", got)
	}

	// Same model without a session sends neither header.
	ct2 := &captureTransport{
		status: 200,
		body: "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n",
	}
	provider2 := &OpenAIProvider{APIKey: "", Client: &http.Client{Transport: ct2}}
	stream2, err := provider2.Stream(context.Background(),
		Model{ID: "glm-4.7-free", BaseURL: "https://opencode.ai/zen/v1"},
		Request{})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, stream2)
	if got := ct2.req.Header.Get("x-opencode-session"); got != "" {
		t.Errorf("x-opencode-session = %q, want empty without session", got)
	}
}

func TestZenHeadersSentOnResponsesWire(t *testing.T) {
	ct := &captureTransport{
		status: 200,
		body: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n",
	}
	provider := &OpenAIProvider{APIKey: "", Client: &http.Client{Transport: ct}}
	stream, err := provider.Stream(context.Background(),
		Model{ID: "muse-spark-1.3-contributor-free", BaseURL: "https://opencode.ai/zen/v1", SessionID: "sess-9"},
		Request{})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, stream)
	if terminal := terminalEvent(t, events); terminal.Type != "done" {
		t.Fatalf("terminal = %#v, want done", terminal)
	}
	if ct.req == nil {
		t.Fatal("transport saw no request")
	}
	if !strings.HasSuffix(ct.req.URL.Path, "/responses") {
		t.Errorf("request path = %q, want suffix /responses", ct.req.URL.Path)
	}
	if got := ct.req.Header.Get("x-opencode-session"); got != "sess-9" {
		t.Errorf("x-opencode-session = %q, want sess-9", got)
	}
	if got := ct.req.Header.Get("x-opencode-client"); got != "myagent" {
		t.Errorf("x-opencode-client = %q, want myagent", got)
	}
}

type captureTransport struct {
	req    *http.Request
	status int
	body   string
}

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.req = r
	return &http.Response{
		StatusCode: c.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Request:    r,
	}, nil
}

func TestParseTransport(t *testing.T) {
	for in, want := range map[string]Transport{
		"":                 TransportAuto,
		"auto":             TransportAuto,
		"AUTO":             TransportAuto,
		"chat-completions": TransportChatCompletions,
		"chat":             TransportChatCompletions,
		"responses":        TransportResponses,
		"response":         TransportResponses,
		" Responses ":      TransportResponses,
	} {
		got, err := ParseTransport(in)
		if err != nil {
			t.Errorf("ParseTransport(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseTransport(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"carrier-pigeon", "rest", "graphql"} {
		if _, err := ParseTransport(in); err == nil {
			t.Errorf("ParseTransport(%q) succeeded, want error", in)
		}
	}
}

func TestModelUsesResponses(t *testing.T) {
	cases := []struct {
		name  string
		model Model
		want  bool
	}{
		{"auto muse uses responses", Model{ID: "muse-spark-1.3"}, true},
		{"auto other uses chat", Model{ID: "gpt-5.5"}, false},
		{"explicit responses wins for other", Model{ID: "gpt-5.5", Transport: TransportResponses}, true},
		{"explicit chat wins for muse", Model{ID: "muse-spark-1.3", Transport: TransportChatCompletions}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.model.UsesResponses(); got != tc.want {
				t.Fatalf("UsesResponses() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExplicitTransportOverridesRouting(t *testing.T) {
	chatBody := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	responsesBody := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"

	// Non-Muse model forced to Responses.
	ct := &captureTransport{status: 200, body: responsesBody}
	p := &OpenAIProvider{APIKey: "", Client: &http.Client{Transport: ct}}
	stream, err := p.Stream(context.Background(),
		Model{ID: "gpt-5.5", BaseURL: "https://api.openai.com/v1", Transport: TransportResponses},
		Request{})
	if err != nil {
		t.Fatal(err)
	}
	if terminal := terminalEvent(t, collect(t, stream)); terminal.Type != "done" {
		t.Fatalf("terminal = %#v, want done", terminal)
	}
	if !strings.HasSuffix(ct.req.URL.Path, "/responses") {
		t.Errorf("path = %q, want suffix /responses", ct.req.URL.Path)
	}

	// Muse model forced back to Chat Completions.
	ct2 := &captureTransport{status: 200, body: chatBody}
	p2 := &OpenAIProvider{APIKey: "", Client: &http.Client{Transport: ct2}}
	stream2, err := p2.Stream(context.Background(),
		Model{ID: "muse-spark-1.3", BaseURL: "https://opencode.ai/zen/v1", Transport: TransportChatCompletions},
		Request{})
	if err != nil {
		t.Fatal(err)
	}
	if terminal := terminalEvent(t, collect(t, stream2)); terminal.Type != "done" {
		t.Fatalf("terminal = %#v, want done", terminal)
	}
	if !strings.HasSuffix(ct2.req.URL.Path, "/chat/completions") {
		t.Errorf("path = %q, want suffix /chat/completions", ct2.req.URL.Path)
	}
}
