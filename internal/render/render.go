// Package render turns collected rows into a plain table.
// It is separate from the TUI: this side only handles write-once output.
package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HeadState is how HEAD relates to the default branch. Plain output ignores
// it (the HEAD column is never colored), but the TUI presents it differently.
type HeadState int

const (
	HeadUnknown  HeadState = iota // no baseline (origin/HEAD) available, or not collected yet
	HeadDefault                   // sitting on the default branch
	HeadBranch                    // on a branch other than the default
	HeadDetached                  // not on a branch at all
)

// String is the name the JSON output uses for the state.
func (s HeadState) String() string {
	switch s {
	case HeadDefault:
		return "default"
	case HeadBranch:
		return "branch"
	case HeadDetached:
		return "detached"
	default:
		return "unknown"
	}
}

// Row is the drift display values for a single repo, all preformatted strings.
type Row struct {
	Group string
	Repo  string
	Head  string
	// HeadState gives meaning to what Head holds, so nobody has to infer it from the string
	HeadState HeadState
	// Clean: on the default branch, nothing changed, the default branch in
	// sync with origin, and no other branch anywhere
	Clean bool

	Changed    string
	Main       string // "↑<push> ↓<pull>" for the default branch against origin
	WIP        string
	LocalOnly  string // "<n>" or "<n> · <gone> gone"
	RemoteOnly string
}

// BranchRow is one branch of a repo, for the TUI's branch list. All values
// are preformatted strings.
type BranchRow struct {
	Name       string // "* " marks the checked-out branch
	Where      string // default / wip / local-only / remote-only, plus " · gone"
	Kind       string // Where without the gone suffix, for coloring
	Push       string
	Pull       string
	LastCommit string
}

// Absent reports the values to mute: unreadable, not applicable, or not
// collected yet. 0 is not absent — it is an observed fact that keeps its
// character — but it shares the dim tone, so only movement carries color.
func Absent(v string) bool { return v == "-" || v == "…" || v == "" }

type palette struct {
	dim, yellow, cyan, green, red, off string
}

type column struct {
	header string
	value  func(Row) string
	tone   func(v string, p palette) string
}

// Numeric columns. Only REPO / HEAD are variable-width and left-aligned; the rest are right-aligned.
var driftColumns = []column{
	// Exists only locally = unrecoverable once lost
	{"CHANGED", func(r Row) string { return r.Changed }, func(_ string, p palette) string { return p.yellow }},
	// Unpushed or unpulled commits on the default branch already disagree with
	// what another machine sees
	{"MAIN", func(r Row) string { return r.Main }, func(_ string, p palette) string { return p.red }},
	{"WIP", func(r Row) string { return r.WIP }, func(_ string, p palette) string { return p.cyan }},
	// Exists only locally, like CHANGED
	{"LOCAL-ONLY", func(r Row) string { return r.LocalOnly }, func(_ string, p palette) string { return p.yellow }},
	{"REMOTE-ONLY", func(r Row) string { return r.RemoteOnly }, func(_ string, p palette) string { return p.red }},
}

// MainSynced is the MAIN value of a default branch in sync with origin.
const MainSynced = "↑0 ↓0"

// Quiet reports a measured value with no movement: 0, or a synced MAIN.
// It shares the dim tone with Absent, so only movement carries color.
func Quiet(v string) bool { return v == "0" || v == MainSynced }

// Table writes the drift table, repeating the heading and column headers per group.
func Table(w io.Writer, rows []Row, useColor bool) {
	if len(rows) == 0 {
		return
	}
	p := newPalette(useColor)

	wrepo := width("REPO", rows, func(r Row) string { return r.Repo })
	whead := width("HEAD", rows, func(r Row) string { return r.Head })
	widths := make([]int, len(driftColumns))
	for i, c := range driftColumns {
		widths[i] = width(c.header, rows, c.value)
	}

	var head strings.Builder
	fmt.Fprintf(&head, "  %s  %s", padRight("REPO", wrepo), padRight("HEAD", whead))
	for i, c := range driftColumns {
		fmt.Fprintf(&head, "  %s", padLeft(c.header, widths[i]))
	}

	group := ""
	for i, r := range rows {
		// Repeat the column headers per group too, so no one has to read far away to tell which cell is which column
		if r.Group != group {
			group = r.Group
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s\n%s\n", group, head.String())
		}

		cells := make([]string, len(driftColumns))
		for j, c := range driftColumns {
			v := c.value(r)
			// Absent and 0 sink into dim; numbers with movement take the column color
			tone := ""
			if Absent(v) || Quiet(v) {
				tone = p.dim
			} else {
				tone = c.tone(v, p)
			}
			cells[j] = paint(padLeft(v, widths[j]), tone, p.off)
		}
		fmt.Fprintf(w, "  %s  %s  %s\n", padRight(r.Repo, wrepo), padRight(r.Head, whead), strings.Join(cells, "  "))
	}
}

func newPalette(useColor bool) palette {
	if !useColor {
		return palette{}
	}
	return palette{
		dim:    "\x1b[90m",
		yellow: "\x1b[33m",
		cyan:   "\x1b[36m",
		green:  "\x1b[32m",
		red:    "\x1b[31m",
		off:    "\x1b[0m",
	}
}

func paint(s, tone, off string) string {
	if tone == "" {
		return s
	}
	return tone + s + off
}

func width[T any](header string, rows []T, value func(T) string) int {
	w := lipgloss.Width(header)
	for _, r := range rows {
		if n := lipgloss.Width(value(r)); n > w {
			w = n
		}
	}
	return w
}

func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(0, w-lipgloss.Width(s))) + s
}
