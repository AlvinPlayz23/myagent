// Package rpc implements minimal JSON-RPC 2.0 framing for myagent's server
// mode. One JSON object per transport message (no batching, no Content-Length
// headers) — the WebSocket transport delivers exactly one Request or Response
// per text frame.
//
// The package is transport-agnostic and io-free: it only parses and builds
// wire structs. Error codes follow the JSON-RPC 2.0 spec plus an application
// range for session errors.
package rpc

import (
	"encoding/json"
	"fmt"
)

// Version is the fixed jsonrpc protocol version string.
const Version = "2.0"

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Application error codes (server range -32000..-32099).
const (
	CodeSessionNotFound   = -32000
	CodeSessionBusy       = -32001
	CodeNotOwner          = -32002
	CodeAgentError        = -32003
	CodeSessionNotRunning = -32004
)

// Request is a client→server JSON-RPC request (or notification when ID is
// absent).
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports whether the request carries no id (fire-and-forget).
func (r *Request) IsNotification() bool { return len(r.ID) == 0 || string(r.ID) == "null" }

// Response is a server→client reply to a Request.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Notification is a server→client push message (no id, no reply expected).
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Error is a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string { return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message) }

// NewError builds an Error with a formatted message.
func NewError(code int, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Parse decodes a single JSON-RPC request frame. It returns a *Error suitable
// for sending back to the client when the frame is malformed.
func Parse(data []byte) (*Request, *Error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, NewError(CodeParseError, "parse error: %v", err)
	}
	if req.JSONRPC != Version {
		return nil, NewError(CodeInvalidRequest, "invalid request: jsonrpc must be %q", Version)
	}
	if req.Method == "" {
		return nil, NewError(CodeInvalidRequest, "invalid request: missing method")
	}
	return &req, nil
}

// MarshalResult builds and encodes a success Response for the given request id.
func MarshalResult(id json.RawMessage, result any) ([]byte, error) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return json.Marshal(Response{JSONRPC: Version, ID: id, Result: result})
}

// MarshalError builds and encodes an error Response for the given request id.
func MarshalError(id json.RawMessage, rpcErr *Error) ([]byte, error) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return json.Marshal(Response{JSONRPC: Version, ID: id, Error: rpcErr})
}

// MarshalNotification builds and encodes a server→client notification.
func MarshalNotification(method string, params any) ([]byte, error) {
	return json.Marshal(Notification{JSONRPC: Version, Method: method, Params: params})
}

// UnmarshalParams decodes request params into dst, returning an invalid-params
// error on failure. Empty params leave dst at its zero value.
func UnmarshalParams(params json.RawMessage, dst any) *Error {
	if len(params) == 0 {
		return nil
	}
	if err := json.Unmarshal(params, dst); err != nil {
		return NewError(CodeInvalidParams, "invalid params: %v", err)
	}
	return nil
}
