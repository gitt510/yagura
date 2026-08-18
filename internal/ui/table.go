package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gitt510/yagura/internal/render"
	"github.com/gitt510/yagura/internal/rows"
)

// Borders are drawn with box-drawing characters. They are shape rather than
// color, so they survive NO_COLOR.
const (
	cornerTL = "╭"
	cornerTR = "╮"
	cornerBL = "╰"
	cornerBR = "╯"
	barH     = "─"
	barV     = "│"
	teeL     = "├"
	teeR     = "┤"
	teeUp    = "┴"
	teeDown  = "┬"
	cross    = "┼"

	cursorMark = "▸"

	// indent is the left margin of the screen, aligned vertically with the leading
	// space of the status bar
	indent = " "
)

type colDef struct {
	header string
	right  bool
	value  func(render.Row) string
	style  func(v string, th theme) lipgloss.Style
}

func toneOf(fixed func(theme) lipgloss.Style) func(string, theme) lipgloss.Style {
	return func(v string, th theme) lipgloss.Style {
		if v == rows.Unsynced {
			return th.warn
		}
		// 0 keeps its character (measured, unlike the structural -) but sinks
		// into dim, so only movement carries color
		if render.Absent(v) || v == "0" {
			return th.dim
		}
		return fixed(th)
	}
}

var driftCols = []colDef{
	{"REPO", false, func(r render.Row) string { return r.Repo }, nil},
	{"HEAD", false, func(r render.Row) string { return r.Head }, nil},
	{"CHANGED", true, func(r render.Row) string { return r.Changed }, toneOf(func(t theme) lipgloss.Style { return t.local })},
	// Unpushed commits are an inconsistency that has already happened as seen from
	// another machine
	{"AHEAD", true, func(r render.Row) string { return r.Ahead }, toneOf(func(t theme) lipgloss.Style { return t.danger })},
	// Falling behind the remote reads as "needs action" too, so it shares red
	{"BEHIND", true, func(r render.Row) string { return r.Behind }, toneOf(func(t theme) lipgloss.Style { return t.danger })},
	{"UNMERGED", true, func(r render.Row) string { return r.Unmerged }, toneOf(func(t theme) lipgloss.Style { return t.remote })},
}

// The default branch is the normal case, so it keeps the plain color; only a
// branch off it stands out.
func headTone(s render.HeadState, th theme) lipgloss.Style {
	switch s {
	case render.HeadDefault:
		return lipgloss.NewStyle()
	case render.HeadBranch:
		return th.headOn
	case render.HeadDetached:
		return th.headOff
	default:
		return lipgloss.NewStyle()
	}
}

type lineKind int

const (
	lineChrome lineKind = iota // Borders, headings, blank lines; the cursor never stops here
	lineRepo
	lineNote // Data cells rendered like lineRepo, but the cursor never stops here
)

type cell struct {
	text  string
	style lipgloss.Style
}

type tableLine struct {
	kind  lineKind
	group string
	text  string // Chrome lines come pre-assembled
	cells []cell
	// ref is the position in the source data; -1 for chrome lines
	ref int
}

type table struct {
	lines  []tableLine
	widths []int
	th     theme
}

// buildTable stacks an independent bordered table per group.
// Column widths are shared across all groups, so the column positions do not
// move as the cursor crosses from one table to the next.
func buildTable(rs []render.Row, th theme) table {
	cols := driftCols

	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = maxWidth(c.header, rs, c.value)
	}

	t := table{widths: widths, th: th}
	counts := groupCounts(rs)

	group := ""
	for i, r := range rs {
		if r.Group != group {
			if group != "" {
				t.push(group, t.bottom())
			}
			group = r.Group
			t.push(group, "")
			t.push(group, th.group.Render(group)+th.groupCount.Render(" · "+countLabel(counts[group], "repo")))
			t.push(group, t.top())
			t.push(group, t.headerRow(headersOf(cols), rightsOf(cols)))
			t.push(group, t.rule())
		}

		cells := make([]cell, len(cols))
		for i, c := range cols {
			v := c.value(r)
			cells[i] = cell{text: pad(v, widths[i], c.right), style: cellStyle(c, r, v, th)}
		}
		kind := lineRepo
		if r.Sub {
			kind = lineNote
		}
		t.lines = append(t.lines, tableLine{kind: kind, group: group, cells: cells, ref: i})
	}
	if group != "" {
		t.push(group, t.bottom())
	}
	return t
}

// cellStyle leaves REPO / HEAD out of the column definitions and derives them
// from the row's state instead.
func cellStyle(c colDef, r render.Row, v string, th theme) lipgloss.Style {
	switch c.header {
	case "REPO":
		return lipgloss.NewStyle()
	case "HEAD":
		return headTone(r.HeadState, th)
	default:
		return c.style(v, th)
	}
}

func (t *table) push(group, text string) {
	t.lines = append(t.lines, tableLine{kind: lineChrome, group: group, text: text, ref: -1})
}

// top is the upper rule. It takes the columns with ┬, symmetric to the ┴ on the
// bottom rule. The table's name goes outside the border.
func (t table) top() string {
	return t.th.border.Render(cornerTL + strings.Join(t.dashes(), teeDown) + cornerTR)
}

func (t table) rule() string {
	return t.th.border.Render(teeL + strings.Join(t.dashes(), cross) + teeR)
}

func (t table) bottom() string {
	return t.th.border.Render(cornerBL + strings.Join(t.dashes(), teeUp) + cornerBR)
}

func (t table) dashes() []string {
	segs := make([]string, len(t.widths))
	for i, w := range t.widths {
		segs[i] = strings.Repeat(barH, w+2)
	}
	return segs
}

func (t table) headerRow(headers []string, rights []bool) string {
	var b strings.Builder
	for i, h := range headers {
		b.WriteString(t.th.border.Render(barV))
		b.WriteString(t.th.header.Render(" " + pad(h, t.widths[i], rights[i]) + " "))
	}
	b.WriteString(t.th.border.Render(barV))
	return b.String()
}

func headersOf(cols []colDef) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.header
	}
	return out
}

func rightsOf(cols []colDef) []bool {
	out := make([]bool, len(cols))
	for i, c := range cols {
		out[i] = c.right
	}
	return out
}

// renderLine assembles one line. When selected, the background covers only the
// inside of the border. Every line gets the indent, aligning the screen's left
// vertical line with the status bar.
func (t table) renderLine(i int, selected bool, width int) string {
	l := t.lines[i]
	if l.kind == lineChrome {
		return clip(indent+l.text, width)
	}

	pads := lipgloss.NewStyle()
	innerBar := t.th.border
	if selected {
		pads = t.th.selected(pads)
		innerBar = t.th.selected(innerBar)
	}

	var b strings.Builder
	b.WriteString(indent)
	for i, c := range l.cells {
		// The outer border keeps its plain color; the inner dividers inherit the
		// background so the band is not cut
		if i == 0 {
			b.WriteString(t.th.border.Render(barV))
		} else {
			b.WriteString(innerBar.Render(barV))
		}

		// The cursor mark takes over the left padding so the columns do not shift
		leftPad := pads
		left := " "
		if i == 0 && selected {
			left, leftPad = cursorMark, innerBar
		}
		st := c.style
		if selected {
			st = t.th.selected(st)
		}
		b.WriteString(leftPad.Render(left))
		b.WriteString(st.Render(c.text))
		b.WriteString(pads.Render(" "))
	}
	b.WriteString(t.th.border.Render(barV))
	return clip(b.String(), width)
}

func countLabel(n int, unit string) string {
	if n != 1 {
		unit += "s"
	}
	return strconv.Itoa(n) + " " + unit
}

func groupCounts(rs []render.Row) map[string]int {
	c := map[string]int{}
	for _, r := range rs {
		if !r.Sub {
			c[r.Group]++
		}
	}
	return c
}

func maxWidth(header string, rs []render.Row, value func(render.Row) string) int {
	w := lipgloss.Width(header)
	for _, r := range rs {
		if n := lipgloss.Width(value(r)); n > w {
			w = n
		}
	}
	return w
}

// Width is counted as display width, not rune count; paths may contain
// full-width characters.
func pad(s string, w int, right bool) string {
	fill := w - lipgloss.Width(s)
	if fill <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", fill) + s
	}
	return s + strings.Repeat(" ", fill)
}

// clip cuts at the terminal width; letting lines wrap would throw off the line
// count.
func clip(s string, width int) string {
	if width <= 0 {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
