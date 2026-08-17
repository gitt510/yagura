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

// Row is the drift display values for a single repo, all preformatted strings.
type Row struct {
	Group string
	Repo  string
	Head  string
	// HeadState gives meaning to what Head holds, so nobody has to infer it from the string
	HeadState HeadState
	// Sub marks an information row of the repo above (branch mode): group
	// counts skip it and the TUI cursor never stops on it, keeping repo and
	// focusable row 1:1
	Sub bool

	Changed  string
	Ahead    string
	Behind   string
	Unmerged string
}

// SessionRow is the display values for a single session, all preformatted
// strings. Cmd is the table's group (which command it is). Branch / Changed
// are filled in only when cwd was inside a repo, and arrive as - when outside.
type SessionRow struct {
	Cmd       string
	CWD       string
	Branch    string
	HeadState HeadState
	Changed   string
	Tmux      string
	PID       string
	Elapsed   string
	CPU       string
	CPUPct    int
	Mem       string
}

// meterWidth is the number of cells in the CPU meter.
const meterWidth = 5

// CPUMeter turns %cpu into a fixed-width bar. Even 1% lights one cell, to
// show it is alive. Above 100% (multiple cores) it caps out full.
func CPUMeter(pct int) string {
	fill := (pct*meterWidth + 99) / 100
	if fill < 0 {
		fill = 0
	}
	if fill > meterWidth {
		fill = meterWidth
	}
	return strings.Repeat("█", fill) + strings.Repeat("░", meterWidth-fill)
}

// SessionColumns is the column definition for the sessions view, shared by
// plain and TUI. Cmd is emitted as a group heading rather than a column.
var SessionColumns = []struct {
	Header string
	Right  bool
	Value  func(SessionRow) string
}{
	{"CWD", false, func(r SessionRow) string { return r.CWD }},
	{"BRANCH", false, func(r SessionRow) string { return r.Branch }},
	{"CHANGED", true, func(r SessionRow) string { return r.Changed }},
	{"TMUX", false, func(r SessionRow) string { return r.Tmux }},
	{"PID", true, func(r SessionRow) string { return r.PID }},
	{"ELAPSED", true, func(r SessionRow) string { return r.Elapsed }},
	// Meter first, % second at a fixed width, so the meters line up even right-aligned
	{"CPU", true, func(r SessionRow) string { return CPUMeter(r.CPUPct) + " " + padLeft(r.CPU, 4) }},
	{"MEM", true, func(r SessionRow) string { return r.Mem }},
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
	// Unpushed commits are, from another machine, an inconsistency that already exists
	{"AHEAD", func(r Row) string { return r.Ahead }, func(_ string, p palette) string { return p.red }},
	// Falling behind the remote reads as "needs action" too, so it shares red
	{"BEHIND", func(r Row) string { return r.Behind }, func(_ string, p palette) string { return p.red }},
	{"UNMERGED", func(r Row) string { return r.Unmerged }, func(_ string, p palette) string { return p.cyan }},
}

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
			if Absent(v) || v == "0" {
				tone = p.dim
			} else {
				tone = c.tone(v, p)
			}
			cells[j] = paint(padLeft(v, widths[j]), tone, p.off)
		}
		fmt.Fprintf(w, "  %s  %s  %s\n", padRight(r.Repo, wrepo), padRight(r.Head, whead), strings.Join(cells, "  "))
	}
}

// SessionTable writes the session table, repeating the heading and column
// headers per group (command). Same grammar as Table for repos.
func SessionTable(w io.Writer, rows []SessionRow, useColor bool) {
	if len(rows) == 0 {
		return
	}
	p := newPalette(useColor)

	widths := make([]int, len(SessionColumns))
	for i, c := range SessionColumns {
		widths[i] = width(c.Header, rows, c.Value)
	}

	cells := make([]string, len(SessionColumns))
	for i, c := range SessionColumns {
		cells[i] = alignProc(c.Header, widths[i], c.Right)
	}
	head := "  " + strings.Join(cells, "  ")

	group := ""
	for i, r := range rows {
		if r.Cmd != group {
			group = r.Cmd
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s\n%s\n", group, head)
		}
		for i, c := range SessionColumns {
			v := c.Value(r)
			cells[i] = paint(alignProc(v, widths[i], c.Right), sessionTone(c.Header, r, v, p), p.off)
		}
		fmt.Fprintf(w, "  %s\n", strings.Join(cells, "  "))
	}
}

// sessionTone is the coloring of the sessions view in plain output. CHANGED
// gets the same "needs attention" yellow as drift. CPU makes no judgement; it
// only mutes an idle 0%.
func sessionTone(header string, r SessionRow, v string, p palette) string {
	switch header {
	case "CPU":
		if r.CPU == "-" {
			return p.dim
		}
		return ""
	case "CHANGED":
		if Absent(v) || v == "0" {
			return p.dim
		}
		return p.yellow
	default:
		if Absent(v) {
			return p.dim
		}
		return ""
	}
}

func alignProc(v string, w int, right bool) string {
	if right {
		return padLeft(v, w)
	}
	return padRight(v, w)
}

// Notice writes a supplementary line (dim) that does not break the table.
func Notice(w io.Writer, msg string, useColor bool) {
	p := newPalette(useColor)
	fmt.Fprintln(w, paint(msg, p.dim, p.off))
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
