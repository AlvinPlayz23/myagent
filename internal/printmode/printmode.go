// Package printmode implements the non-interactive "-p / --print" mode: it runs
// a single prompt to completion, streaming assistant text to stdout as it
// arrives and printing concise tool activity to stderr. No TUI.
package printmode

import (
	"context"
	"fmt"
	"io"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/session"
	"github.com/myagent/myagent/internal/types"
)

// Run executes a single prompt in print mode. Assistant text is written to
// stdout; tool activity is written to stderr. Every produced message is
// appended to sess (if non-nil).
func Run(ctx context.Context, cfg agent.Config, sess *session.Session, history []types.Message, prompt string, stdout, stderr io.Writer) error {
	var textStreamed bool
	sink := func(_ context.Context, ev types.AgentEvent) error {
		switch ev.Type {
		case types.EventMessageUpdate:
			ame := ev.AssistantMessageEvent
			if ame == nil {
				return nil
			}
			if ame.Type == "text_delta" && ame.Delta != "" {
				fmt.Fprint(stdout, ame.Delta)
				textStreamed = true
			}
		case types.EventToolExecutionStart:
			fmt.Fprintf(stderr, "\n[tool] %s %v\n", ev.ToolName, ev.Args)
		case types.EventToolExecutionEnd:
			if ev.IsError {
				fmt.Fprintf(stderr, "[tool:%s] error\n", ev.ToolName)
			}
		case types.EventMessageEnd:
			// Terminate the streamed assistant text with a single newline, but only
			// if we actually streamed text this message (avoids stray blank lines
			// for tool-only assistant turns).
			if ev.Message != nil && ev.Message.Role == types.RoleAssistant && textStreamed {
				fmt.Fprintln(stdout)
				textStreamed = false
			}
		}
		return nil
	}

	loop := agent.New(cfg, history, sink)
	produced, err := loop.Run(ctx, []types.Message{userMessage(prompt)})
	if err != nil {
		return err
	}

	if sess != nil {
		for _, m := range produced {
			if perr := sess.AppendMessage(m); perr != nil {
				return perr
			}
		}
	}
	return nil
}

func userMessage(text string) types.Message {
	return types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{types.TextBlock(text)},
	}
}
