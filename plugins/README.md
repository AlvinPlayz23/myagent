# Example plugins

Two example plugins for `myagent` (`plugins.json`-only system, see `../PLUGINS.md`).

| File             | What it is                                                              |
| ---------------- | ----------------------------------------------------------------------- |
| `plugins.json`   | Both plugins combined — copy this to install                            |
| `plan-mode.json` | Just the PLAN-mode profile                                              |
| `test.json`      | Just the `/test` command (prints `Hellow orld` — misspelling intended) |

## The plugins

### PLAN-mode (`plan` profile)

Read-only planning mode (named `plan`, so `/plan` works as sugar for
`/profile plan`):

- Tool allowlist: `read` + `bash` only — the model cannot `write`/`edit`.
- `bashDeny` blocks `rm -rf` and `git push` with an error result.
- Appends `PLAN MODE` instructions to the system prompt.
- Sets effort to `medium`.

Use it: `/plan` (or `/plan on`) to enter, `/plan off` to leave. `/profile plan`
and `/profile reset` work too → picker) to leave. The footer shows `profile:plan` while active.

### `/test` (run command)

Executes `echo Hellow orld` locally and renders stdout as a local
transcript block. Never sent to the model. (Yes, `Hellow orld` is spelled
that way on purpose.)

## Install

The loader only reads these two paths (see `PLUGINS.md` §2) — this folder
is just examples, it is NOT loaded directly:

| File                      | Purpose                                                     |
| ------------------------- | ----------------------------------------------------------- |
| `~/.myagent/plugins.json` | Global plugins (`~/.myagent` = `config.Dir()`)              |
| `./.myagent/plugins.json` | Project-local plugins. Wins on name collision.              |

```bash
# Global (all projects):
cp plugins/plugins.json ~/.myagent/plugins.json

# Or project-local (this repo only):
mkdir -p .myagent
cp plugins/plugins.json .myagent/plugins.json
```

Then verify:

```bash
go run . plugin validate ~/.myagent/plugins.json
go run . plugin list
```

Startup will print e.g. `Loaded plugins: 1 command (/test), 1 profile (plan)`.
