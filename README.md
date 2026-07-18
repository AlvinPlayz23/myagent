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
# First run opens setup, which creates ~/.myagent/config.json.
# Interactive TUI (default mode, no args)
go run .

# Quick smoke (no API key needed)
go run . sessions        # list persisted sessions, exits cleanly
```

Everything below assumes you `cd`-ed into this repo.

---

## Prerequisites 

- **Go** — see `go.mod` for the minimum toolchain version.
- An **OpenAI-compatible API key**. First run collects it in the setup wizard.
  The default endpoint is `https://api.openai.com/v1` and the default model is
  `gpt-4o`.
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

### First-run setup

Configuration is required. When you start interactive myagent and
`$MYAGENT_DIR/config.json` does not exist or is empty, a terminal wizard asks
for your API key, base URL, and model, then creates the file. The API-key input
is masked. Press **Esc** on the blank API-key field or **Ctrl+C** to cancel.

The wizard requires a real terminal; `-p`/`--print` will instead fail with a
message telling you to run `myagent` once and complete setup.

If `config.json` already contains any non-whitespace content, setup is not
shown and myagent reads that file normally. Invalid JSON or a missing API key
in an existing file remains a configuration error, so myagent never overwrites
an existing configuration unexpectedly.

### Config file (`$MYAGENT_DIR/config.json`)

```json
{
  "apiKey": "sk-...",
  "baseUrl": "https://api.openai.com/v1",
  "model": "gpt-4o"
}
```

The wizard creates parent directories as required. On Unix, the file is stored
with `0600` permissions because it contains the API key.

### Environment overrides

| Variable           | Purpose                              | Default                     |
| ------------------ | ------------------------------------ | --------------------------- |
| `OPENAI_API_KEY`   | API key override                     | —                           |
| `OPENAI_BASE_URL`  | Endpoint base URL                    | `https://api.openai.com/v1` |
| `MYAGENT_MODEL`    | Model id                             | `gpt-4o`                    |
| `MYAGENT_DIR`      | Config + session directory           | `~/.myagent`                |
| `MYAGENT_SHELL`    | Shell used by the `bash` tool        | auto-detected (see below)   |

Environment variables override values loaded from `config.json` for that run;
they do not remove the need to complete first-run setup. This supports CI and
temporary provider/model changes without changing the saved configuration.

### Shell selection for the `bash` tool

The `bash` tool runs commands through a real shell. It resolves one in
this order:

1. **`MYAGENT_SHELL`** — used verbatim if set (e.g.
   `C:\Program Files\Git\bin\bash.exe`, `pwsh`, `/bin/bash`).
2. On **Windows**, a real **Git Bash / MSYS2 `bash.exe`** — probed in the
   usual Git-for-Windows install locations and on `PATH`. The
   `C:\Windows\System32\bash.exe` **WSL launcher stub is deliberately
   skipped**, so the tool never shells into WSL.
3. On **Windows** with no real bash found, **`cmd.exe`** (via `%ComSpec%`).
4. On macOS / Linux, `/bin/sh`.

Install [Git for Windows][gitwin] to get bash-style commands
(`ls`, `grep`, `rg`, `&&` chains) working natively; otherwise commands
run under `cmd.exe`.

[gitwin]: https://git-scm.com/download/win

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

### TUI slash commands

Slash commands run locally in the TUI. They are not sent to the model or
stored as user messages. Commands that change conversation state are available
only while the agent is idle. Typing `/` opens the command list; continue
typing to filter it, use **Up / Down** to select, **Tab** to complete, **Enter**
to run, or **Esc** to dismiss it.

| Command              | Action                                                  |
| -------------------- | ------------------------------------------------------- |
| `/help`              | Show available commands and keybindings                 |
| `/model-id <id>`     | Use a model for subsequent turns in the current session |
| `/compact`           | Force context compaction when a safe boundary exists    |
| `/clear`             | Clear the visible transcript; retain conversation context |
| `/new`               | Start a fresh persisted conversation                    |
| `/resume`            | Open the session selector and resume a previous conversation |

`/model-id` is a session-only override. It does not update `config.json`.
`/new` preserves the previous session file and makes the new session the one
shown in the exit resume instructions. `/resume` lists previous sessions by
timestamp, ID, and prompt preview; use **Up / Down**, **Enter**, or **Esc** to
navigate, resume, or cancel.

---

## Sessions

Each run creates or resumes a JSONL file under
`$MYAGENT_DIR/sessions/<id>.jsonl`. The file is append-only: line 1 is a
session header (`type`, `version`, `id`, `cwd`, `timestamp`); each
following line is a message or compaction entry linked to the previous one
by `id` / `parentId`. Killing myagent mid-run is safe — re-running with
`--continue` (or `--resume-id <id>`) restores the full conversation.

After leaving the interactive TUI, myagent prints the session's `--resume-id`
and `--resume` commands. Session paths inside your home directory are shown
with a `~` prefix.

### Context compaction

Long sessions are automatically compacted before they overflow the model's
context window. When the estimated context size nears **230 000 tokens**
(the harness limit is 256 000, with a 26 000-token reserve), the agent
summarizes the older conversation history using the same model and a
dedicated summarization prompt (no tools), then replaces it with
`[summary] + [recent messages]`. Approximately 20 000 tokens of recent
context are kept verbatim so the agent retains its immediate working state.

The compaction is persisted to the session file as a `compaction` entry.
On resume, only `[summary] + [kept messages]` are loaded — the compacted-away
messages remain on disk for audit but are not sent to the model. Repeated
compactions update the existing summary rather than re-summarizing from
scratch.

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
│   │   └── compaction/  # auto context compaction (summarize old history)
│   ├── config/          # JSON config + env overrides
│   ├── eventbus/        # guaranteed-delivery pub/sub for agent events
│   ├── llm/             # Provider interface + OpenAI streaming adapter
│   ├── printmode/       # non-interactive one-shot driver
│   ├── session/         # JSONL persistence (v4, id/parentId chain, compaction, list, resume)
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
Complete the first-run wizard by running `myagent`, or add `apiKey` to an
existing `$MYAGENT_DIR/config.json`. `OPENAI_API_KEY` can override it for one
run.

**`myagent: no API key configured: run myagent once to complete setup`**
`-p` / `--print` cannot display the terminal wizard. Run `myagent` with no
prompt in an interactive terminal, complete setup, then rerun the command.

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

**On Windows, the `bash` tool errors with "wsl is not available" (or runs in the wrong environment)**
That means it found `C:\Windows\System32\bash.exe`, the WSL launcher
stub. myagent now skips that stub automatically and prefers Git Bash,
falling back to `cmd.exe`. Install [Git for Windows][gitwin] for full
bash-style commands, or set `MYAGENT_SHELL` to the shell you want.

**`go run . --continue` says "no sessions found"**
Either none exist yet (run an interactive session first), or
`MYAGENT_DIR` differs between the runs/lists. Use the same value or
`myagent sessions` (with the var set) to confirm.

---

## License

Same upstream license as the `pi/` reference (see that directory).
