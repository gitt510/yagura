// yagura is a local lookout. It tabulates the sessions of running agent
// CLIs and the drift of the repos under the declared roots.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/gitt510/yagura/internal/config"
	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
	"github.com/gitt510/yagura/internal/procs"
	"github.com/gitt510/yagura/internal/render"
	"github.com/gitt510/yagura/internal/rows"
	"github.com/gitt510/yagura/internal/ui"
)

// concurrency is how many repos are fetched / inspected at once.
const concurrency = 12

const usage = `usage: yagura [query] [--root <dir>]... [--sessions] [-n|--no-fetch] [--plain] [--interval <dur>]

  query           filter repos by substring match on path (repos view only)
      --root      root to watch; repeatable; overrides the config file
      --sessions  start on the sessions view instead of repos
  -n, --no-fetch  skip fetch and report from recorded remote-tracking refs
      --plain     print the table once without the TUI (auto on non-TTY)
      --interval  refresh interval of the repos view for this run (overrides config)
`

func setupMessage() string {
	return fmt.Sprintf(`no roots declared.

create %s like:

%s
for a one-off run, --root <dir> works without the file.
`, config.Path(), config.Skeleton())
}

func main() {
	os.Exit(run())
}

func run() int {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// flags are one-off overrides; they win over the declaration (config)
	if opts.interval != 0 {
		cfg.Repos.Interval = config.Duration(opts.interval)
	}

	// the sessions view is not tied to the repo declaration (no root or query)
	if opts.withSessions && (opts.plain || !isTTY()) {
		return plainSessions(cfg.Sessions.Commands)
	}

	roots := opts.roots
	if len(roots) == 0 {
		// any --root discards the config file roots (a one-off override)
		roots = cfg.Repos.Roots
	}

	repos, warnings := discover.Repos(roots, opts.query)
	reposNote := ""
	if len(repos) == 0 {
		reposNote = "no repos match: " + opts.query
		if len(roots) == 0 {
			reposNote = "no roots declared: create " + config.Path()
		}

		// nothing to show at the default entry (repos view): guide and exit, no TUI
		if !opts.withSessions {
			for _, w := range warnings {
				fmt.Fprintln(os.Stderr, w)
			}
			if len(roots) == 0 {
				fmt.Fprint(os.Stderr, setupMessage())
			} else {
				fmt.Fprintf(os.Stderr, "no repos match: %s\n", opts.query)
			}
			return 1
		}
	}

	if opts.plain || !isTTY() {
		return plain(repos, warnings, opts)
	}

	err = ui.Run(ui.Options{
		Repos:            repos,
		Commands:         cfg.Sessions.Commands,
		SessionsInterval: time.Duration(cfg.Sessions.Interval),
		ReposInterval:    time.Duration(cfg.Repos.Interval),
		Roots:            len(roots),
		Warnings:         warnings,
		ReposNote:        reposNote,
		NoFetch:          opts.noFetch,
		WithSessions:     opts.withSessions,
		Color:            !noColor(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func plainSessions(commands []string) int {
	ps := procs.List(commands)
	git := gitinfo.ForDirs(rows.CWDs(ps), concurrency)
	useColor := isTTY() && !noColor()

	home, _ := os.UserHomeDir()
	render.SessionTable(os.Stdout, rows.Sessions(ps, home, git), useColor)
	if len(ps) == 0 {
		render.Notice(os.Stdout, "no sessions running", useColor)
	}
	return 0
}

func plain(repos []discover.Repo, warnings []string, opts options) int {
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, w)
	}

	paths := make([]string, len(repos))
	for i, r := range repos {
		paths[i] = r.Path
	}

	var failed []string
	var failedPaths []string
	if !opts.noFetch {
		byPath := map[string]string{}
		for _, r := range repos {
			byPath[r.Path] = r.Name
		}
		failedPaths = gitinfo.Fetch(paths, concurrency)
		for _, p := range failedPaths {
			failed = append(failed, byPath[p])
		}
	}

	infos := gitinfo.CollectAll(paths, concurrency)
	idx := map[string]int{}
	for i, p := range paths {
		idx[p] = i
	}
	for _, p := range failedPaths {
		infos[idx[p]].FetchFailed = true
	}

	useColor := isTTY() && !noColor()
	render.Table(os.Stdout, rows.Build(repos, infos), useColor)

	if opts.noFetch {
		fmt.Println("(no fetch — AHEAD/BEHIND reflect recorded remote-tracking refs)")
	} else if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "[warn] fetch failed: %s\n", strings.Join(failed, ", "))
		return 1
	}
	return 0
}

type options struct {
	query        string
	roots        []string
	noFetch      bool
	withSessions bool
	plain        bool
	interval     time.Duration
}

func parseArgs(args []string) (options, error) {
	var opts options
	var positional []string

	// flags that take a value accept both `--x v` and `--x=v`
	value := func(i *int, name string) (string, error) {
		if v, ok := strings.CutPrefix(args[*i], name+"="); ok {
			return v, nil
		}
		if *i+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", name)
		}
		*i++
		return args[*i], nil
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-n" || a == "--no-fetch":
			opts.noFetch = true
		case a == "--sessions":
			opts.withSessions = true
		case a == "--plain":
			opts.plain = true
		case a == "--root" || strings.HasPrefix(a, "--root="):
			v, err := value(&i, "--root")
			if err != nil {
				return options{}, err
			}
			opts.roots = append(opts.roots, v)
		case a == "--interval" || strings.HasPrefix(a, "--interval="):
			v, err := value(&i, "--interval")
			if err != nil {
				return options{}, err
			}
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return options{}, fmt.Errorf("invalid --interval: %s", v)
			}
			opts.interval = d
		case strings.HasPrefix(a, "-") && a != "-":
			return options{}, fmt.Errorf("unknown flag: %s", a)
		default:
			positional = append(positional, a)
		}
	}

	if len(positional) > 1 {
		return options{}, fmt.Errorf("only one query allowed: %s", strings.Join(positional, " "))
	}
	if len(positional) == 1 {
		opts.query = positional[0]
	}
	return opts, nil
}

// isTTY reports whether stdout is a terminal, via isatty. ModeCharDevice
// would call /dev/null a terminal too, so `yagura > /dev/null` would try to
// open the TUI and fail.
func isTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func noColor() bool {
	_, ok := os.LookupEnv("NO_COLOR")
	return ok
}
