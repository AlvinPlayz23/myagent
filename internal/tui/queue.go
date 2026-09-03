// Package tui implements the interactive terminal pager: a from-scratch
// engine (internal/tui/engine) drives a full-screen transcript with an
// accent-rail chrome, a bordered composer, and modal pickers, fed by the same
// AgentEvent stream as print mode.
package tui

import (
	"github.com/AlvinPlayz23/myagent/internal/agent"
)

// msgQueue is the shared concurrency-safe agent.MessageQueue implementation,
// hoisted to internal/agent so server mode can reuse it. The alias keeps the
// tui package's historical name for its call sites.
type msgQueue = agent.Queue

// newMsgQueue returns an empty queue.
func newMsgQueue() *msgQueue { return agent.NewQueue() }
