# myagent

A Go coding agent. Headless **print mode** for one-shot prompts, and an
interactive **TUI** built on [bubbletea v2][btea] for multi-turn work. Speaks
the OpenAI streaming protocol against any compatible endpoint (OpenAI,
Ollama, LM Studio, vLLM, etc.).

See [`myagent-plan.md`](./myagent-plan.md) for the full design plan and
shipped status.

[btea]: https://github.com/charmbracelet/bubbletea

---

## TL;DR

```bash
# Pick any OpenAI-compatible endpoint
export OPENAI_API_KEY=sk-...
export OPENAI_BASE_URL=https://api.openai.com/v1   # default
export MYAGENT_MODEL=gpt-4o                        # default

# Interactive TUI (default mode, no args)
go run .

# Quick smoke (no API key needed)
go run . sessions        # list persisted sessions, exits cleanly
```

Everything below assumes you `cd`-ed into this repo.

---

## Prerequisites

- **Go** — see `go.mod` for the minimum toolchain version.
- An **OpenAI-compatible API key + endpoint**. Defaults assume
  `https://api.openai.com/v1` and the `gpt-4o` model.
- A real terminal for the TUI:
  - macOS / Linux: any modern terminal.
  - Windows: **Windows Terminal** (ConPTY + 24-bit color). PowerShell
    ISE won't render the alt-screen UI.

---

## Setup

```bash
git clone <repo-url> myagent
cd myagent
go mod download         # pull bubble/lipgloss/glamour deps
```

There are no other install steps.

---

## Configuration

### Environment variables (override everything)

| Variable           | Purpose                              | Default                     |
| ------------------ | ------------------------------------ | --------------------------- |
| `OPENAI_API_KEY`   | API key (required)                   | —                           |
| `OPENAI_BASE_URL`  | Endpoint base URL                    | `https://api.openai.com/v1` |
| `MYAGENT_MODEL`    | Model id                             | `gpt-4o`                    |
| `MYAGENT_DIR`      | Config + session directory           | `~/.myagent`                |

### Optional config file (`$MYAGENT_DIR/config.json`)

```json
{
  "apiKey":  "sk-...",
  "baseUrl": "https://api.openai.com/v1",
  "model":   "gpt-4o"
}
```

A missing file is not an error: env vars + defaults still produce a
working config. Env vars always win over file values.

> Prefer the env var for the API key on shared machines.

---

## Usage

| Command                                      | What it does                                       |
| -------------------------------------------- | -------------------------------------------------- |
| `go run .`                                   | Enter the interactive TUI (default)                |
| `go run . tui`                               | Same, explicit                                     |
| `go run . -p "..."`                          | One-shot prompt; streams reply to stdout           |
| `go run . -p "..." --model=claude-sonnet-4.5`| Override model for this run                        |
| `go run . --continue`                        | Resume the most recently modified session          |
| `go run . --resume ./path/session.jsonl`     | Resume by file path                                |
| `go run . --resume-id <uuid>`                | Resume by session id                               |
| `go run . sessions`                          | List persisted sessions, newest first              |

### Print-mode usage notes

`-p` accepts both flag syntax (`-p "…"` or `-print "…"`) and a single
trailing positional argument:

```bash
go run . -p "Write a haiku about Go."
go run . --print "Write a haiku about Go."
go run . "Write a haiku about Go."   # same thing
```

### TUI keybindings

| Key                  | Action                                                |
| -------------------- | ----------------------------------------------------- |
| **Enter**            | Send (steer if a turn is currently running)           |
| **Alt+Enter**        | Send as a follow-up (runs after the current turn)     |
| **Esc**              | Abort the current turn                                |
| **Ctrl+C**           | Quit                                                  |
| **Ctrl+O**           | Expand / collapse all tool blocks                     |
| **PgUp / PgDn**      | Scroll transcript                                     |
| **Tab**              | Focus the input area                                  |

---

## Sessions

Each run creates or resumes a JSONL file under
`$MYAGENT_DIR/sessions/<id>.jsonl`. The file is append-only: line 1 is a
session header (`type`, `version`, `id`, `cwd`, `timestamp`); each
following line is a message entry linked to the previous one by
`id` / `parentId`. Killing myagent mid-run is safe — re-running with
`--continue` (or `--resume-id <id>`) restores the full conversation.

List sessions (newest first by file mtime):

```bash
$ go run . sessions
ID                                     MSGS  MODIFIED             PREVIEW
0192f3…                                12    2025-07-17 14:22:01  explain why this is fast
0191d4…                                3     2025-07-17 13:11:14  Write a haiku about Go
```

If you just created sessions in a non-default `MYAGENT_DIR`, export the
same var when listing:

```bash
MYAGENT_DIR=/tmp/foo go run . sessions
```

---

## Project layout

```
.
├── main.go              # CLI entry point
├── go.mod / go.sum
├── internal/
│   ├── agent/           # prompt→stream→tools→repeat loop, event emission
│   ├── config/          # JSON config + env overrides
│   ├── eventbus/        # guaranteed-delivery pub/sub for agent events
│   ├── llm/             # Provider interface + OpenAI streaming adapter
│   ├── printmode/       # non-interactive one-shot driver
│   ├── session/         # JSONL persistence (v3, id/parentId chain, list, resume)
│   ├── tools/           # read / write / edit / bash tools + truncation utils
│   ├── tui/             # bubbletea v2 UI: transcript, input, footer
│   └── types/           # Message / Content / ToolCall / Usage / Event
├── myagent-plan.md      # design plan
├── pi/                  # upstream TypeScript implementation (reference)
└── README.md            # this file
```

---

## Development

### Build / vet / test

```bash
go build ./...                   # compile everything
go vet ./...                     # static analysis
go test ./...                    # unit tests (event bus + session hardening)

# Binary at the project root (Windows→myagent.exe, *nix→myagent)
go build -o ./myagent.exe .
```

`go test -race ./...` requires cgo. On Windows that isn't enabled by
default — install a C toolchain (e.g. `scoop install gcc`, or use WSL) to
enable race-detected test runs.

### Common tasks

```bash
# Smoke (no API key)
go run . sessions

# One-shot in dev (no rebuild needed)
go run . -p "summarize the readme"

# Build once, run the binary
go build -o ./myagent.exe .
./myagent.exe -p "hi"
```

Watch + restart on save (optional helper, if you have `entr`):

```bash
find . -name '*.go' | entr -c go run . -p "what changed?"
```

### Adding a new tool

1. Implement a struct that satisfies `tools.Tool` (`internal/tools/tool.go`).
2. Register it in `tools.DefaultRegistry` (`internal/tools/default.go`).
3. The system prompt in `internal/agent/system_prompt.go` lists the
   available tools automatically — no other wiring needed.

### Adding a new LLM provider

1. Implement the `llm.Provider` interface (`internal/llm/provider.go`).
2. Wire it into `main.go` alongside (or instead of)
   `llm.NewOpenAIProvider`.

The agent loop is provider-agnostic; it consumes `types.AgentEvent`s.

---

## Troubleshooting

**`myagent: no API key: set OPENAI_API_KEY (or apiKey in config.json)`**
Set the env var, or put `apiKey` into `$MYAGENT_DIR/config.json`.

**Fresh build fails with `glamour: ansi.Style … does not implement …`**
A transitive dep version conflict between `glamour` and `bubbletea v2`.
Upgrade `charmbracelet/x/cellbuf` + `x/ansi` to latest:

```bash
go get github.com/charmbracelet/x/cellbuf@latest github.com/charmbracelet/x/ansi@latest
go mod tidy
```

**TUI exits immediately on Windows**
You're likely under `cmd.exe` or PowerShell ISE, neither of which
supports ConPTY. Use **Windows Terminal** or run under WSL.

**CI logs show nothing for `go run . tui`**
The TUI deliberately uses bubbletea's alt screen — output goes to the
buffer and is restored on exit. For CI-friendly logs, capture the
binary's stdout via `go run . -p "..."` instead.

**`go run . --continue` says "no sessions found"**
Either none exist yet (run an interactive session first), or
`MYAGENT_DIR` differs between the runs/lists. Use the same value or
`myagent sessions` (with the var set) to confirm.

---

## License

Same upstream license as the `pi/` reference (see that directory).
