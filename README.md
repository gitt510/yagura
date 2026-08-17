# yagura

A lookout tower for local work: the drift of every repo under your declared
roots, and the agent CLI sessions running on the machine right now.

## Views

- The TUI opens on the repos view; `p` switches between repos and sessions
- `?` opens the key list as a floating panel; `r` refreshes the current view; `q` quits
- Each view auto-refreshes on its own interval, independently of the other
- On a non-TTY, the selected view's table is printed once instead
- Colors follow the terminal theme (ANSI-16 only); `NO_COLOR` disables them

## Repos view

- Repos are grouped by declared root
- A root that is itself a git repo is watched as one; otherwise its direct children are watched, without recursion
- Columns: `HEAD`, `CHANGED` (working-tree changes), `AHEAD` / `BEHIND` (against the upstream), `UNMERGED` (commits not on `origin/HEAD`)
- Every refresh fetches each repo with `--prune`
- When a fetch fails, `AHEAD` / `BEHIND` / `UNMERGED` show `x` instead of stale numbers
- Without any declared root, startup exits with setup instructions
- `tab` toggles branch mode: one row per local branch instead of one per repo — the default branch always first, the rest by name; the checked-out branch alone carries `CHANGED`, and the cursor sits on the row with the repo name; `AHEAD` / `BEHIND` count against each branch's own upstream, `UNMERGED` against `origin/HEAD`

## Sessions view

- Running processes whose command basename matches `sessions.commands` are listed, grouped by command
- When a session's cwd is inside a git work tree, `BRANCH` and `CHANGED` show that repo's state; collection never fetches
- `TMUX` shows the pane (`session:window.pane`) the process runs in; `-` outside tmux
- `CPU` shows an instantaneous meter and percentage from `ps`

## Requirements

- `git`, `ps`, and `lsof` in `PATH`
- `tmux` is optional; without it the `TMUX` column shows `-`

## Setup

- `just install` (or `go install .`) puts the `yagura` binary into `GOBIN`
- Create the config file before first use of the repos view

## Usage

```sh
yagura                   # TUI, repos view
yagura --sessions        # TUI, sessions view
yagura gh- --plain -n    # one-shot repos table, filtered, without fetch
```

## Configuration

- Config file: `~/.config/yagura/config.toml` (`$XDG_CONFIG_HOME/yagura/config.toml` when set)
- A commented example lives at `internal/config/config.example.toml`; the setup message prints the same text
- Unknown keys and non-positive intervals are rejected at startup

| key | default | effect |
| --- | --- | --- |
| `repos.roots` | — | roots to watch |
| `repos.interval` | `"1m"` | refresh interval of the repos view |
| `sessions.commands` | `["claude"]` | process names to watch, matched against the command basename |
| `sessions.interval` | `"10s"` | refresh interval of the sessions view |

| flag | effect |
| --- | --- |
| `query` (positional) | filter repos by substring match on path |
| `--root <dir>` | watch this root for the run instead of `repos.roots`; repeatable |
| `--sessions` | start on the sessions view |
| `-n`, `--no-fetch` | skip fetch; remote-derived columns reflect recorded remote-tracking refs |
| `--plain` | print the table once without the TUI |
| `--interval <dur>` | override `repos.interval` for this run |

## Development

- `just test` runs the test suite; `just check` runs gofmt, go vet, and golangci-lint
- `mise` pins the toolchain (go, golangci-lint)
- Colors are written as ANSI-16 slot numbers only; a guard test rejects 256-color, truecolor, and hex
- User-facing string literals are English-only, enforced by golangci-lint (gosmopolitan)
