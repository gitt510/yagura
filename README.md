# yagura

A lookout tower for local work: the drift of every repo under your declared
roots, and the agent CLI sessions running on the machine right now.

![yagura repos view: a drift table with branch mode toggled by tab](docs/demo.gif)

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
- `enter` opens the focused repo as a new window in the `repos.tmux-session` session (created if missing), with the repo path as cwd; the outcome lands in the footer
- `tab` toggles branch mode: one row per local branch instead of one per repo — the default branch always first, the rest by name; the checked-out branch alone carries `CHANGED`, and the cursor sits on the row with the repo name; `AHEAD` / `BEHIND` count against each branch's own upstream, `UNMERGED` against `origin/HEAD`

## Sessions view

- Running processes whose command basename matches `sessions.commands` are listed, grouped by command
- When a session's cwd is inside a git work tree, `BRANCH` and `CHANGED` show that repo's state; collection never fetches
- `TMUX` shows the pane (`session:window.pane`) the process runs in; `-` outside tmux
- `CPU` shows an instantaneous meter and percentage from `ps`

## JSON output

- `--json` prints the selected view once as a single JSON document, in place of the table
- Counts are numbers; a count with nothing to compare against — no upstream, or a fetch that failed — is `null`, never `0`
- `fetch_failed` tells the two apart: `true` means the remote is unknown, not absent
- Each repo carries its absolute `path`, so a reader can act on it directly
- A failed fetch is reported in the document (`fetch_failed`, `warnings`), not as a non-zero exit
- Lists are always lists: no repos means `"repos": []`

```json
{
  "repos": [
    {
      "root": "~/ghq/github.com/gitt510",
      "name": "moat",
      "path": "/Users/tg/ghq/github.com/gitt510/moat",
      "head": "main",
      "head_state": "default",
      "changed": 0,
      "ahead": 1,
      "behind": 0,
      "unmerged": 1,
      "fetch_failed": false
    }
  ],
  "warnings": []
}
```

- `head_state` is one of `default`, `branch`, `detached`, `unknown` (no `origin/HEAD` to compare against)
- `--sessions --json` yields `{"sessions": [...], "warnings": []}`, one record per session, with `branch` / `changed` `null` outside a work tree

## Requirements

- `git`, `ps`, and `lsof` in `PATH`
- `tmux` is optional; without it the `TMUX` column shows `-`, and it is required only when `enter` opens a repo

## Setup

- `just install` (or `go install .`) puts the `yagura` binary into `GOBIN`
- Create the config file before first use of the repos view

## Usage

```sh
yagura                   # TUI, repos view
yagura --sessions        # TUI, sessions view
yagura gh- --plain -n    # one-shot repos table, filtered, without fetch
yagura --json            # one-shot repos document, for a script or an agent
```

```sh
# every repo with local work or drift
yagura --json | jq '.repos[] | select(.changed > 0 or .behind > 0 or .unmerged > 0)'
```

## Configuration

- Config file: `~/.config/yagura/config.toml` (`$XDG_CONFIG_HOME/yagura/config.toml` when set)
- A commented example lives at `internal/config/config.example.toml`; the setup message prints the same text
- Unknown keys and non-positive intervals are rejected at startup

| key | default | effect |
| --- | --- | --- |
| `repos.roots` | — | roots to watch |
| `repos.interval` | `"1m"` | refresh interval of the repos view |
| `repos.tmux-session` | — | tmux session that `enter` opens repos into; unset keeps `enter` inert |
| `sessions.commands` | `["claude"]` | process names to watch, matched against the command basename |
| `sessions.interval` | `"10s"` | refresh interval of the sessions view |

| flag | effect |
| --- | --- |
| `query` (positional) | filter repos by substring match on path |
| `--root <dir>` | watch this root for the run instead of `repos.roots`; repeatable |
| `--sessions` | start on the sessions view |
| `-n`, `--no-fetch` | skip fetch; remote-derived columns reflect recorded remote-tracking refs |
| `--plain` | print the table once without the TUI |
| `--json` | print the same facts once as JSON; wins over `--plain` |
| `--interval <dur>` | override `repos.interval` for this run |
| `-h`, `--help` | print the usage to stdout and exit 0 |

## Development

- `just test` runs the test suite; `just check` runs gofmt, go vet, and golangci-lint
- `just screenshot` rebuilds `docs/demo.gif` from a synthetic fixture (`docs/fixture.sh`), so the demo shows no real paths; it requires `vhs`
- `mise` pins the toolchain (go, golangci-lint)
- Colors are written as ANSI-16 slot numbers only; a guard test rejects 256-color, truecolor, and hex
- User-facing string literals are English-only, enforced by golangci-lint (gosmopolitan)
