// Package tui implements the interactive terminal UI (bubbletea v2), porting
// the observable UX of pi's interactive mode: a scrolling transcript of
// user/assistant/tool blocks, a multi-line input editor, and a status/footer
// bar, driven by the same AgentEvent stream as print mode.
package tui

import (
	"github.com/myagent/myagent/internal/agent"
)

// msgQueue is the shared concurrency-safe agent.MessageQueue implementation,
// hoisted to internal/agent so server mode can reuse it. The alias keeps the
// tui package's historical name for its call sites.
type msgQueue = agent.Queue

// newMsgQueue returns an empty queue.
func newMsgQueue() *msgQueue { return agent.NewQueue() }
