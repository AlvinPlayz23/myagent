# myagent Plugins Guide

No Go, no compiling. You edit one JSON file (`plugins.json`) plus scripts you already have (Python, shell, `gh`, etc.), and `myagent` turns each entry into something usable at startup: a new tool for the model, a new slash command for you, or a named mode (profile).

Works the same in the TUI, `myagent -p "..."`, and `myagent serve`.

> `SKILL.md` skills are a separate system and aren't covered here.

## What you can make

| Kind | What it is | Example |
| ---- | ---------- | ------- |
| **Tool** | A new tool the model can call, backed by a shell command | `joke` → runs `python scripts/joke.py --topic ...` |
| **Command** | A new TUI slash command, either `prompt` (ask the model) or `run` (run locally) | `/poem` → asks for a poem, `/log` → runs `git log` |
| **Profile** | A named mode: tool allowlist + extra instructions + optional `effort`/`model` override | `plan` → read-only planning mode |

Minimal file — this gives you all three:

```json
{
  "enabled": true,
  "tools": [
    {
      "name": "joke",
      "description": "Tell a joke about a topic",
      "parameters": {
        "type": "object",
        "properties": { "topic": { "type": "string" } },
        "required": ["topic"]
      },
      "command": "python C:/scripts/joke.py --topic {{.topic}}"
    }
  ],
  "commands": [
    { "name": "/poem", "description": "Write a poem about <args>", "prompt": "Write a short poem about {{$args}}." },
    { "name": "/log", "description": "Show recent git log", "run": "git log --oneline -10" }
  ],
  "profiles": [
    {
      "name": "plan",
      "description": "Read-only planning mode",
      "tools": ["read", "bash"],
      "bashDeny": ["rm\\s+-rf", "git\\s+push"],
      "instructions": "You are in PLAN MODE. Explore and propose a plan. Do NOT edit/write files.",
      "effort": "medium"
    }
  ]
}
```

Result: the model gains a `joke` tool, the TUI gains `/poem` and `/log` (in the picker and `/help`), and `/profile plan` (or `/plan`) enters planning mode.

## 5-minute quickstart

1. Create your global file (pick one):

   ```bash
   # Windows (PowerShell)
   notepad $HOME\.myagent\plugins.json

   # macOS / Linux
   ${EDITOR:-nano} ~/.myagent/plugins.json
   ```

   Or project-local (this repo only):

   ```bash
   mkdir -p .myagent
   # copy the combined example:
   cp plugins/plugins.json .myagent/plugins.json
   ```

   See `plugins/` in this repo for ready-to-copy files: `plugins.json` (both examples combined), `plan-mode.json` (just the profile), `test.json` (just a `/test` command).

2. Paste the minimal file above (or one of the examples).

3. Verify it:

   ```bash
   myagent plugin validate ~/.myagent/plugins.json
   myagent plugin list
   ```

4. Start `myagent`. You should see something like:

   ```text
   Loaded plugins: 1 tool (joke), 2 commands (/poem, /log), 1 profile (plan)
   ```

   If something was skipped, you'll also see `warning: ...` lines — valid entries still load. Fix and re-validate.

## Where `plugins.json` lives

| File | Purpose |
| ---- | ------- |
| `~/.myagent/plugins.json` | Global — applies everywhere. `~/.myagent` is the same folder as `config.json`. |
| `./.myagent/plugins.json` | Project-local — optional, applies in that folder. Wins on name collision. |

Rules:

- Missing file = fine, not an error. Empty file or `{}` = no plugins.
- Unknown top-level fields are ignored (so old files keep working).
- **Duplicates within one file:** first entry wins, rest are skipped with a warning.
- **Same `name` in both files:** the project entry wins, with a warning.
- **Invalid entries never crash startup** — they're skipped with a warning, the rest still load.

### Turning plugins off

Any of these disables everything:

- `"enabled": false` at the top of *either* file (kill-switch — the project file can't re-enable a global `false`, and vice versa).
- `myagent --no-plugins` (TUI, `-p`, and `serve` all accept it).

## Tools: give the model new abilities

A tool is a shell command template. The model fills in the arguments, `myagent` renders the command, runs it, and returns stdout/stderr to the model.

```json
{
  "name": "joke",
  "description": "Tell a joke about a topic",
  "parameters": {
    "type": "object",
    "properties": { "topic": { "type": "string" } },
    "required": ["topic"]
  },
  "command": "python C:/scripts/joke.py --topic {{.topic}}",
  "timeoutMs": 15000
}
```

| Field | Required | Rules |
| ----- | -------- | ----- |
| `name` | yes | Lowercase letters, digits, `-`. Must start with a letter/digit, max 32 chars. Can't be `read`, `write`, `edit`, or `bash`. |
| `description` | yes | Plain text. Shown to the model and in help. |
| `parameters` | yes | JSON Schema object, `type: "object"`. Stick to `string` / `number` / `boolean` properties for now. |
| `command` | yes | Shell string. Use `{{.argName}}` for args (see below). Non-empty. |
| `timeoutMs` | no | Kill after N ms. Default `30000`, clamped to `1000`–`120000`. |

Tips:

- **Don't quote arguments yourself.** `{{.topic}}` is shell-quoted for you (Windows `cmd.exe` vs Unix `sh` handled automatically). Just write `--topic {{.topic}}`. `{{.cwd}}` and `{{.sessionId}}` are quoted the same way — never wrap placeholders in extra quotes.
- **Copy-paste debuggable.** Args go only through `{{...}}` substitution (stdin isn't used), so you can copy the rendered command from logs and run it yourself.
- **Shell:** same as the `bash` tool — `MYAGENT_SHELL` if set, else Git Bash → `cmd.exe` on Windows, `/bin/sh` elsewhere.
- **Timeouts / cancel:** a timeout kills the process; `Esc` (TUI) cancels it. Non-zero exit, timeout, or cancel shows up to the model as a failed tool call.
- **Big output:** capped at ~50KB / 2000 lines (same as `bash`). The full output is saved to a temp file when truncated — check the tool details for the path.

## Commands: your own slash commands

Pick **exactly one** of `prompt` or `run`:

```json
{ "name": "/poem", "description": "Write a poem about <args>", "prompt": "Write a short poem about {{$args}}. Cwd is {{.cwd}}." },
{ "name": "/log", "description": "Show recent git log", "run": "git log --oneline -10" }
```

| Field | Required | Rules |
| ----- | -------- | ----- |
| `name` | yes | Must start with `/`, then the same charset as tools, max 32 chars *including* the `/` (e.g. `/poem`, `/log`). Can't collide with built-ins: `/help`, `/models`, `/effort`, `/providers`, `/customize`, `/compact`, `/clear`, `/new`, `/resume`, `/rename`, `/export`, `/init`, `/thinking`. |
| `description` | yes | Shown in `/help` and the command picker. |
| `prompt` | either | Sent to the model as a normal user message. Can use all tools, including your plugin tools. |
| `run` | either | Run locally, stdout shown as a local transcript block (never sent to the model). Non-zero exit shows as a red/error block. |
| `timeoutMs` | no | Same default/clamp as tools. |

- `prompt` behaves like typing the rendered text yourself (same path as `/init`).
- `run` behaves like a quick local shortcut — good for `git log`, `git status`, test runners, etc.

## Template variables

| Variable | In tools | In commands | Meaning |
| -------- | -------- | ----------- | ------- |
| `{{.argName}}` | ✅ | — | Value of that tool argument, e.g. `{{.topic}}`. Must be a declared `parameters.properties` key (plus `cwd` / `sessionId`, see below). Auto-quoted. |
| `{{$args}}` | — | ✅ | Raw text after the slash command. `/poem the sea` → `the sea`. |
| `{{.cwd}}` | ✅ | ✅ | Session working directory. |
| `{{.sessionId}}` | ✅ | — | Session id, useful for logging. |

Referencing anything else (e.g. `{{.missing}}` that isn't a declared parameter) fails validation and that tool/command is skipped with a warning.

## Profiles: named modes (e.g. `plan`)

A profile filters which tools the model sees, appends extra instructions to the system prompt, and can override `effort` (`provider`/`model` are currently ignored with a warning). The classic example is a read-only planning mode:

```json
{
  "name": "plan",
  "description": "Read-only planning mode",
  "tools": ["read", "bash"],
  "bashDeny": ["rm\\s+-rf", "git\\s+push"],
  "instructions": "You are in PLAN MODE. Explore and propose a plan. Do NOT edit/write files. End with a step list.",
  "effort": "medium"
}
```

| Field | Required | Rules |
| ----- | -------- | ----- |
| `name` | yes | Same charset/length as tools. `plan` is just a convention, not special — but naming one `plan` enables the `/plan` shortcut below. |
| `description` | yes | Shown in the `/profile` picker. |
| `tools` | no | Allowlist of built-ins (`read`, `write`, `edit`, `bash`) plus your plugin tool names. Omitted = all tools. Unknown names are skipped with a warning; the profile is kept. |
| `bashDeny` | no | Up to 20 regexes, each ≤ 200 chars, must compile. Checked against `bash` and plugin-tool commands *before* exec — a match blocks with an error, without running anything. |
| `instructions` | no | Appended to the system prompt under a `Mode instructions:` heading. Max 4000 chars. |
| `provider` | no | Currently ignored (warning at startup); profile keeps tools/instructions/effort. |
| `model` | no | Currently ignored (warning at startup); profile keeps tools/instructions/effort. |
| `effort` | no | One of `minimal`, `low`, `medium`, `high`, `xhigh`, `max`. Empty = provider default. Anything else → profile skipped. |

How enforcement works: excluded tools are *removed* — if the model tries one, it gets `Tool not found`. `bashDeny` is a pre-exec regex block on the rendered command.

### Using profiles

TUI:

- `/profile plan` — activate. `/profile reset` — leave. Bare `/profile` — list profiles (shows the active one).
- If you have a profile literally named `plan`, you also get `/plan`, `/plan on` (activate), `/plan off` (leave).
- The footer shows the active profile, e.g. `model • effort • profile:plan`.
- Switching is per-session. `/new` and `/resume` reset to no profile unless you pinned one at startup. You can't switch mid-run — cancel first.
- Unknown name → error listing what's available, no crash.

CLI / server:

```bash
myagent -p "summarise this repo" --profile plan
myagent serve --profile plan
```

Unknown `--profile` fails fast at startup with `unknown profile "x" (available: ...)`. In the TUI the same mistake shows as a status message and continues with no profile.

The active profile is in-memory (+ the `--profile` flag). It isn't written into the session file.

## Checking what's loaded

```bash
myagent plugin list              # merged global + project for the current dir
myagent plugin validate          # validate the merged files for the current dir
myagent plugin validate PATH     # validate one file
```

- `validate` prints `valid: Loaded plugins: ...` or lists `warning: ...` lines.
- Startup also prints the summary (`Loaded plugins: 1 tool (joke), 2 commands (/poem, /log), 1 profile (plan)`) plus one `warning:` per skipped entry.

## Validation cheat sheet

If your entry doesn't show up, check this first — it's almost always one of these:

- File isn't valid JSON.
- `name` has uppercase, underscores, spaces, or is over 32 chars (`/log` counts the `/`).
- Tool `name` is `read` / `write` / `edit` / `bash`; command `name` is a built-in like `/help`.
- Tool `parameters` is missing or `type` isn't `"object"`.
- `command` / `prompt` / `run` is empty, or both `prompt` and `run` are set (or neither).
- Template references something undeclared: tools allow declared params + `{{.cwd}}` + `{{.sessionId}}`; commands allow `{{$args}}` + `{{.cwd}}`.
- Profile `effort` typo (must be exactly `minimal`, `low`, `medium`, `high`, `xhigh`, `max`), bad regex in `bashDeny`, or `instructions` over 4000 chars.
- `timeoutMs` outside `1000`–`120000` is clamped, not rejected.

## Security: plugins run as you

Same privilege as the `bash` tool — full trust, no sandbox in v1:

- Only local files are read; nothing is downloaded.
- **Review `plugins.json` before installing**, especially project-local files someone else put in a repo.
- Every execution is logged (name, rendered command, duration, exit code).
- The startup summary tells you what got loaded — if a profile appeared you didn't expect, investigate before continuing.

## Full working file

Copy-paste starter (`~/.myagent/plugins.json` for global, `./.myagent/plugins.json` for project-local):

```json
{
  "enabled": true,
  "tools": [
    {
      "name": "joke",
      "description": "Tell a joke about a topic",
      "parameters": {
        "type": "object",
        "properties": { "topic": { "type": "string" } },
        "required": ["topic"]
      },
      "command": "python C:/scripts/joke.py --topic {{.topic}}",
      "timeoutMs": 15000
    }
  ],
  "commands": [
    { "name": "/poem", "description": "Write a poem about <args>", "prompt": "Write a short poem about {{$args}}." },
    { "name": "/log", "description": "Show recent git log", "run": "git log --oneline -10" }
  ],
  "profiles": [
    {
      "name": "plan",
      "description": "Read-only planning mode",
      "tools": ["read", "bash"],
      "bashDeny": ["rm\\s+-rf", "git\\s+push"],
      "instructions": "You are in PLAN MODE. Explore and propose a plan. Do NOT edit/write files.",
      "effort": "medium"
    }
  ]
}
```

More in `plugins/`: combined example, plan-mode-only, and `/test`-only variants.

## FAQ

**Command or tool?**
If *you* invoke it with `/...`, it's a command. If the *model* should invoke it during a run, it's a tool.

**`prompt` or `run`?**
`prompt` when the model needs to see it (draft, explain, rewrite). `run` when you just want local output without bothering the model (logs, status, quick scripts).

**Why is my entry skipped?**
Run `myagent plugin validate` — it tells you exactly which entry and why. Warnings also print at startup.

**Why does the model say "Tool not found"?**
A profile is active and that tool isn't in its `tools` allowlist, or a `bashDeny` pattern blocked the rendered command. Reset with `/profile reset` and retry.

**Project vs global — which wins?**
Project. Same `name` in `./.myagent/plugins.json` replaces the global one (with a warning). Use this to override a global tool per-repo.

**Do I need to restart after editing?**
Yes — files are read once at startup. (Hot-reload is future work.)

**Can I share plugins?**
Yes — send the JSON + scripts. The other person reviews them, drops the JSON into one of the two paths, and runs `plugin validate`. No marketplace in v1.
