# yagura

A lookout tower for local work: the drift of every repo under your declared
roots, and the agent CLI sessions running on the machine right now.

![yagura repos view: a drift table, with one repo opened into its branch list by enter](docs/demo.gif)

## Views

- The TUI opens on the repos view; `p` switches between repos and sessions
- `?` opens the key list as a floating panel; `r` refreshes the current view; `q` quits
- Each view auto-refreshes on its own interval, independently of the other
- On a non-TTY, the selected view's table is printed once instead
- Colors follow the terminal theme (ANSI-16 only); `NO_COLOR` disables them

## Repos view

- Repos are grouped by declared root
- A root that is itself a git repo is watched as one; otherwise its direct children are watched, without recursion
- Columns: `HEAD`, `CHANGED` (working-tree changes), `MAIN` (`↑` unpushed / `↓` unpulled commits of the default branch against `origin`), `WIP` / `LOCAL-ONLY` / `REMOTE-ONLY` (branch counts)
- The default branch is the one `origin/HEAD` points at
- Every other branch is counted in exactly one of `WIP` (local and on `origin`), `LOCAL-ONLY`, `REMOTE-ONLY`, matched by name
- `LOCAL-ONLY` adds `· <n> gone` for branches whose upstream was deleted on the remote; a branch never pushed is not gone
- A repo on its default branch with every count at 0 is clean and in sync: its row is dimmed, and the status bar counts these repos
- Every refresh fetches each repo with `--prune`
- When a fetch fails, `MAIN` / `WIP` / `LOCAL-ONLY` / `REMOTE-ONLY` show `x` instead of stale numbers
- Without any declared root, startup exits with setup instructions
- `enter` (or `l`) opens the focused repo's branch list; `esc` (or `h`) steps back to the repos table
- The branch list shows the working tree in one line, then one row per branch: `BRANCH` (`*` on the checked-out one), `WHERE` (`default` / `wip` / `local-only` / `remote-only`, plus `· gone`), `PUSH ↑` / `PULL ↓` (against `origin/<same name>`; `-` when either side is missing), `LAST COMMIT`
- The default branch leads the branch list; the rest follow by name
- `o` opens the focused repo, or the one whose branches are shown, as a new window in the `repos.tmux-session` session (created if missing), with the repo path as cwd; the outcome lands in the footer

## Sessions view

- Running processes whose command basename matches `sessions.commands` are listed, grouped by command
- When a session's cwd is inside a git work tree, `BRANCH` and `CHANGED` show that repo's state; collection never fetches
- `TMUX` shows the pane (`session:window.pane`) the process runs in; `-` outside tmux
- `CPU` shows an instantaneous meter and percentage from `ps`

## JSON output

- `--json` prints the selected view once as a single JSON document, in place of the table
- Counts are numbers; a count with nothing to compare against — no default branch, a branch missing on one side, or a fetch that failed — is `null`, never `0`
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
      "main_ahead": 1,
      "main_behind": 0,
      "wip": 0,
      "local_only": 1,
      "gone": 1,
      "remote_only": 0,
      "branches": [
        {"name": "main", "where": "default", "current": true, "gone": false, "push": 1, "pull": 0, "last_commit": "2026-10-08"},
        {"name": "fix/typo", "where": "local-only", "current": false, "gone": true, "push": null, "pull": null, "last_commit": "2026-10-01"}
      ],
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
- `tmux` is optional; without it the `TMUX` column shows `-`, and it is required only when `o` opens a repo

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
yagura --json | jq '.repos[] | select(.changed > 0 or .main_ahead > 0 or .main_behind > 0 or .wip > 0 or .local_only > 0 or .remote_only > 0)'
```

## Configuration

- Config file: `~/.config/yagura/config.toml` (`$XDG_CONFIG_HOME/yagura/config.toml` when set)
- A commented example lives at `internal/config/config.example.toml`; the setup message prints the same text
- Unknown keys and non-positive intervals are rejected at startup

| key | default | effect |
| --- | --- | --- |
| `repos.roots` | — | roots to watch |
| `repos.interval` | `"1m"` | refresh interval of the repos view |
| `repos.tmux-session` | — | tmux session that `o` opens repos into; unset keeps `o` inert |
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
