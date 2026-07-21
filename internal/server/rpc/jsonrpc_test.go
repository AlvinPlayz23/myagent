package rpc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	req, rpcErr := Parse([]byte(`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"cwd":"/tmp"}}`))
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	if req.Method != "session.create" {
		t.Errorf("method = %q, want session.create", req.Method)
	}
	if string(req.ID) != "1" {
		t.Errorf("id = %q, want 1", req.ID)
	}
	if req.IsNotification() {
		t.Error("IsNotification() = true for request with id")
	}
}

func TestParseNotification(t *testing.T) {
	req, rpcErr := Parse([]byte(`{"jsonrpc":"2.0","method":"ping"}`))
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	if !req.IsNotification() {
		t.Error("IsNotification() = false for request without id")
	}
}

func TestParseStringID(t *testing.T) {
	req, rpcErr := Parse([]byte(`{"jsonrpc":"2.0","id":"abc-1","method":"m"}`))
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	if string(req.ID) != `"abc-1"` {
		t.Errorf("id = %q, want %q", req.ID, `"abc-1"`)
	}
}

func TestParseMalformed(t *testing.T) {
	cases := []struct {
		name string
		in   string
		code int
	}{
		{"invalid json", `{not json`, CodeParseError},
		{"empty", ``, CodeParseError},
		{"wrong version", `{"jsonrpc":"1.0","method":"m"}`, CodeInvalidRequest},
		{"missing version", `{"method":"m"}`, CodeInvalidRequest},
		{"missing method", `{"jsonrpc":"2.0","id":1}`, CodeInvalidRequest},
		{"array batch", `[{"jsonrpc":"2.0","method":"m"}]`, CodeParseError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, rpcErr := Parse([]byte(tc.in))
			if rpcErr == nil {
				t.Fatal("expected error, got nil")
			}
			if rpcErr.Code != tc.code {
				t.Errorf("code = %d, want %d", rpcErr.Code, tc.code)
			}
		})
	}
}

func TestMarshalResult(t *testing.T) {
	b, err := MarshalResult(json.RawMessage("7"), map[string]string{"sessionId": "s1"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"jsonrpc":"2.0"`, `"id":7`, `"sessionId":"s1"`} {
		if !strings.Contains(s, want) {
			t.Errorf("result %s missing %s", s, want)
		}
	}
}

func TestMarshalResultNilID(t *testing.T) {
	b, err := MarshalResult(nil, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"id":null`) {
		t.Errorf("result %s missing null id", b)
	}
}

func TestMarshalError(t *testing.T) {
	b, err := MarshalError(json.RawMessage("3"), NewError(CodeSessionNotFound, "no session %q", "x"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"id":3`, `"code":-32000`, `no session \"x\"`} {
		if !strings.Contains(s, want) {
			t.Errorf("error %s missing %s", s, want)
		}
	}
}

func TestMarshalNotification(t *testing.T) {
	b, err := MarshalNotification("session.event", map[string]any{"sessionId": "s1"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, `"id"`) {
		t.Errorf("notification %s must not carry an id", s)
	}
	if !strings.Contains(s, `"method":"session.event"`) {
		t.Errorf("notification %s missing method", s)
	}
}

func TestUnmarshalParams(t *testing.T) {
	var dst struct {
		SessionID string `json:"sessionId"`
	}
	if rpcErr := UnmarshalParams(json.RawMessage(`{"sessionId":"s1"}`), &dst); rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	if dst.SessionID != "s1" {
		t.Errorf("sessionId = %q, want s1", dst.SessionID)
	}
	if rpcErr := UnmarshalParams(nil, &dst); rpcErr != nil {
		t.Errorf("nil params should be accepted, got %v", rpcErr)
	}
	if rpcErr := UnmarshalParams(json.RawMessage(`"not an object"`), &dst); rpcErr == nil {
		t.Error("expected invalid-params error")
	} else if rpcErr.Code != CodeInvalidParams {
		t.Errorf("code = %d, want %d", rpcErr.Code, CodeInvalidParams)
	}
}
