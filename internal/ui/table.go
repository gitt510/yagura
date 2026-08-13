package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gitt510/yagura/internal/render"
	"github.com/gitt510/yagura/internal/rows"
)

// 枠は box-drawing で組む。色ではなく形なので NO_COLOR でも残る。
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

	// indent は画面左の margin。status bar の先頭 space と縦を揃える
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
		if render.Absent(v) {
			return th.dim
		}
		if v == "0" {
			return lipgloss.NewStyle()
		}
		return fixed(th)
	}
}

var driftCols = []colDef{
	{"REPO", false, func(r render.Row) string { return r.Repo }, nil},
	{"HEAD", false, func(r render.Row) string { return r.Head }, nil},
	{"CHANGED", true, func(r render.Row) string { return r.Changed }, toneOf(func(t theme) lipgloss.Style { return t.local })},
	// 未 push の commit は他の machine から見て既に起きている不整合
	{"AHEAD", true, func(r render.Row) string { return r.Ahead }, toneOf(func(t theme) lipgloss.Style { return t.danger })},
	{"BEHIND", true, func(r render.Row) string { return r.Behind }, toneOf(func(t theme) lipgloss.Style { return t.remote })},
	{"UNMERGED", true, func(r render.Row) string { return r.Unmerged }, toneOf(func(t theme) lipgloss.Style { return t.remote })},
}

// default branch は平常時なので素の色のまま、外れている branch だけ立てる。
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
	lineChrome lineKind = iota // 枠・見出し・空行。cursor は止まらない
	lineRepo
)

type cell struct {
	text  string
	style lipgloss.Style
}

type tableLine struct {
	kind  lineKind
	group string
	text  string // chrome 行は組み立て済み
	cells []cell
	// ref は元データでの位置。chrome 行は -1
	ref int
}

type table struct {
	lines  []tableLine
	widths []int
	th     theme
}

// buildTable は group ごとに独立した枠付きの表を積む。
// 列幅は全 group で共通にして、cursor が表を渡っても列位置が動かないようにする。
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
		t.lines = append(t.lines, tableLine{kind: lineRepo, group: group, cells: cells, ref: i})
	}
	if group != "" {
		t.push(group, t.bottom())
	}
	return t
}

// cellStyle は REPO / HEAD だけ列定義に持たせず、行の状態から決める。
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

// top は上辺の罫線。下辺の ┴ と対称に ┬ で列を受ける。表の名前は枠の外に出す。
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

// renderLine は 1 行を組む。selected なら枠の内側だけ背景を敷く。
// 全行に indent を敷き、画面左の縦ラインを status bar と揃える。
func (t table) renderLine(i int, selected bool, width int) string {
	l := t.lines[i]
	if l.kind != lineRepo {
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
		// 外枠は素の色のまま。内側の仕切りは背景を継いで帯を切らさない
		if i == 0 {
			b.WriteString(t.th.border.Render(barV))
		} else {
			b.WriteString(innerBar.Render(barV))
		}

		// cursor 印は左の余白を潰して置く。列の開始位置を動かさないため
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
		c[r.Group]++
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

// 幅は rune 数ではなく表示幅で数える。path に全角が入り得る。
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

// clip は端末幅で切る。折り返させると行数が合わなくなる。
func clip(s string, width int) string {
	if width <= 0 {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
