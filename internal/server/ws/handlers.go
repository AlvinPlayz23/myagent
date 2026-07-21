package ws

import (
	"errors"

	"github.com/myagent/myagent/internal/server/core"
	"github.com/myagent/myagent/internal/server/rpc"
	"github.com/myagent/myagent/internal/session"
	"github.com/myagent/myagent/internal/types"
)

// dispatch routes one parsed request to its handler and writes the reply.
func (c *conn) dispatch(req *rpc.Request) {
	result, rpcErr := c.handle(req)
	if req.IsNotification() {
		return // fire-and-forget: no reply even on error
	}
	if rpcErr != nil {
		c.writeError(req.ID, rpcErr)
		return
	}
	c.writeResult(req.ID, result)
}

// sessionRef identifies a session in request params.
type sessionRef struct {
	SessionID string `json:"sessionId"`
}

// sessionText carries a session id plus message text.
type sessionText struct {
	SessionID string `json:"sessionId"`
	Message   string `json:"message"`
}

func (c *conn) handle(req *rpc.Request) (any, *rpc.Error) {
	switch req.Method {
	case "session.create":
		var p struct {
			Cwd      string `json:"cwd"`
			Provider string `json:"provider"`
			Model    string `json:"model"`
		}
		if rpcErr := rpc.UnmarshalParams(req.Params, &p); rpcErr != nil {
			return nil, rpcErr
		}
		ss, err := c.manager.Create(c.id, core.CreateParams{Cwd: p.Cwd, Provider: p.Provider, Model: p.Model})
		if err != nil {
			return nil, coreError(err)
		}
		c.startEventPump(ss)
		return map[string]any{"sessionId": ss.ID(), "model": ss.ModelID(), "cwd": ss.Cwd()}, nil

	case "session.resume":
		var p sessionRef
		if rpcErr := rpc.UnmarshalParams(req.Params, &p); rpcErr != nil {
			return nil, rpcErr
		}
		if p.SessionID == "" {
			return nil, rpc.NewError(rpc.CodeInvalidParams, "sessionId is required")
		}
		ss, err := c.manager.Resume(c.id, p.SessionID)
		if err != nil {
			return nil, coreError(err)
		}
		c.startEventPump(ss)
		return map[string]any{
			"sessionId": ss.ID(),
			"model":     ss.ModelID(),
			"cwd":       ss.Cwd(),
			"messages":  messagesOrEmpty(ss.Messages()),
		}, nil

	case "session.list":
		infos, err := c.manager.List()
		if err != nil {
			return nil, rpc.NewError(rpc.CodeInternalError, "list sessions: %v", err)
		}
		out := make([]map[string]any, 0, len(infos))
		for _, info := range infos {
			out = append(out, sessionInfoJSON(info))
		}
		return map[string]any{"sessions": out}, nil

	case "session.prompt":
		ss, text, rpcErr := c.sessionWithText(req)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if err := ss.Prompt(text); err != nil {
			return nil, coreError(err)
		}
		return map[string]any{}, nil

	case "session.steer":
		ss, text, rpcErr := c.sessionWithText(req)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if err := ss.Steer(text); err != nil {
			return nil, coreError(err)
		}
		return map[string]any{"queued": true}, nil

	case "session.followUp":
		ss, text, rpcErr := c.sessionWithText(req)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if err := ss.FollowUp(text); err != nil {
			return nil, coreError(err)
		}
		return map[string]any{"queued": true}, nil

	case "session.abort":
		ss, rpcErr := c.sessionFromParams(req)
		if rpcErr != nil {
			return nil, rpcErr
		}
		ss.Abort()
		return map[string]any{}, nil

	case "session.compact":
		ss, rpcErr := c.sessionFromParams(req)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if err := ss.Compact(); err != nil {
			return nil, coreError(err)
		}
		return map[string]any{}, nil

	case "session.messages":
		ss, rpcErr := c.sessionFromParams(req)
		if rpcErr != nil {
			return nil, rpcErr
		}
		return map[string]any{"messages": messagesOrEmpty(ss.Messages())}, nil

	case "session.setModel":
		var p struct {
			SessionID string `json:"sessionId"`
			Provider  string `json:"provider"`
			Model     string `json:"model"`
		}
		if rpcErr := rpc.UnmarshalParams(req.Params, &p); rpcErr != nil {
			return nil, rpcErr
		}
		if p.SessionID == "" {
			return nil, rpc.NewError(rpc.CodeInvalidParams, "sessionId is required")
		}
		if err := c.manager.SetModel(c.id, p.SessionID, p.Provider, p.Model); err != nil {
			return nil, coreError(err)
		}
		return map[string]any{}, nil

	case "session.close":
		var p sessionRef
		if rpcErr := rpc.UnmarshalParams(req.Params, &p); rpcErr != nil {
			return nil, rpcErr
		}
		if p.SessionID == "" {
			return nil, rpc.NewError(rpc.CodeInvalidParams, "sessionId is required")
		}
		if err := c.manager.Close(c.id, p.SessionID); err != nil {
			return nil, coreError(err)
		}
		return map[string]any{}, nil

	default:
		return nil, rpc.NewError(rpc.CodeMethodNotFound, "method %q not found", req.Method)
	}
}

// sessionFromParams resolves the session referenced by {sessionId} params.
func (c *conn) sessionFromParams(req *rpc.Request) (*core.ServerSession, *rpc.Error) {
	var p sessionRef
	if rpcErr := rpc.UnmarshalParams(req.Params, &p); rpcErr != nil {
		return nil, rpcErr
	}
	if p.SessionID == "" {
		return nil, rpc.NewError(rpc.CodeInvalidParams, "sessionId is required")
	}
	ss, err := c.manager.Get(c.id, p.SessionID)
	if err != nil {
		return nil, coreError(err)
	}
	return ss, nil
}

// sessionWithText resolves {sessionId, message} params.
func (c *conn) sessionWithText(req *rpc.Request) (*core.ServerSession, string, *rpc.Error) {
	var p sessionText
	if rpcErr := rpc.UnmarshalParams(req.Params, &p); rpcErr != nil {
		return nil, "", rpcErr
	}
	if p.SessionID == "" {
		return nil, "", rpc.NewError(rpc.CodeInvalidParams, "sessionId is required")
	}
	if p.Message == "" {
		return nil, "", rpc.NewError(rpc.CodeInvalidParams, "message is required")
	}
	ss, err := c.manager.Get(c.id, p.SessionID)
	if err != nil {
		return nil, "", coreError(err)
	}
	return ss, p.Message, nil
}

// coreError maps core sentinel errors to JSON-RPC application errors.
func coreError(err error) *rpc.Error {
	switch {
	case errors.Is(err, core.ErrNotFound):
		return rpc.NewError(rpc.CodeSessionNotFound, "%v", err)
	case errors.Is(err, core.ErrBusy):
		return rpc.NewError(rpc.CodeSessionBusy, "%v", err)
	case errors.Is(err, core.ErrNotOwner):
		return rpc.NewError(rpc.CodeNotOwner, "%v", err)
	case errors.Is(err, core.ErrNotRunning):
		return rpc.NewError(rpc.CodeSessionNotRunning, "%v", err)
	case errors.Is(err, core.ErrClosed):
		return rpc.NewError(rpc.CodeSessionNotFound, "%v", err)
	default:
		return rpc.NewError(rpc.CodeAgentError, "%v", err)
	}
}

// messagesOrEmpty guarantees a JSON array (never null) for message lists.
func messagesOrEmpty(msgs []types.Message) []types.Message {
	if msgs == nil {
		return []types.Message{}
	}
	return msgs
}

// sessionInfoJSON maps session.Info to a camelCase wire shape.
func sessionInfoJSON(info session.Info) map[string]any {
	return map[string]any{
		"id":           info.ID,
		"path":         info.Path,
		"cwd":          info.Cwd,
		"created":      info.Created.UTC().Format("2006-01-02T15:04:05.000Z"),
		"modified":     info.Modified.UTC().Format("2006-01-02T15:04:05.000Z"),
		"messageCount": info.MessageCount,
		"preview":      info.Preview,
	}
}
