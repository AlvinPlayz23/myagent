package tui

import (
	"fmt"
	"strings"
)

type commandKind int

const (
	commandHelp commandKind = iota
	commandClear
	commandNew
	commandCompact
	commandModelID
)

type slashCommand struct {
	kind commandKind
	arg  string
}

// parseSlashCommand parses commands handled by the interactive UI. Local
// commands never become conversation messages or reach the model.
func parseSlashCommand(text string) (slashCommand, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return slashCommand{}, fmt.Errorf("not a slash command")
	}

	name := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(text, name))
	switch name {
	case "/help":
		if arg != "" {
			return slashCommand{}, fmt.Errorf("usage: /help")
		}
		return slashCommand{kind: commandHelp}, nil
	case "/clear":
		if arg != "" {
			return slashCommand{}, fmt.Errorf("usage: /clear")
		}
		return slashCommand{kind: commandClear}, nil
	case "/new":
		if arg != "" {
			return slashCommand{}, fmt.Errorf("usage: /new")
		}
		return slashCommand{kind: commandNew}, nil
	case "/compact":
		if arg != "" {
			return slashCommand{}, fmt.Errorf("usage: /compact")
		}
		return slashCommand{kind: commandCompact}, nil
	case "/model-id":
		if arg == "" {
			return slashCommand{}, fmt.Errorf("usage: /model-id <id>")
		}
		return slashCommand{kind: commandModelID, arg: arg}, nil
	default:
		return slashCommand{}, fmt.Errorf("unknown command: %s (try /help)", name)
	}
}

const helpText = `Commands:
  /help                 Show this help
  /model-id <id>        Use a model for subsequent turns in this session
  /compact              Summarize older conversation context now
  /clear                Clear the visible transcript (context is retained)
  /new                  Start a new persisted conversation

Keys: enter send/steer, alt+enter queue follow-up, esc cancel, ctrl+o expand tools, ctrl+c quit`
