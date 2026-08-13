// Package render は収集済みの行を plain table に落とす。
// TUI とは別実装で、こちらは 1 度書いて終わる出力だけを受け持つ。
package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HeadState は HEAD が default branch とどういう関係にあるか。
// plain 出力は使わない (HEAD 列に色を付けないため) が、TUI が見せ方を変える。
type HeadState int

const (
	HeadUnknown  HeadState = iota // 基準 (origin/HEAD) が引けない、または収集前
	HeadDefault                   // default branch の上に居る
	HeadBranch                    // default 以外の branch
	HeadDetached                  // branch に居ない
)

// Row は 1 repo ぶんの drift 表示値。すべて整形済みの文字列。
type Row struct {
	Group string
	Repo  string
	Head  string
	// HeadState は Head の中身の意味づけ。文字列から推測させないために持たせる
	HeadState HeadState

	Changed  string
	Ahead    string
	Behind   string
	Unmerged string
}

// SessionRow は 1 session ぶんの表示値。すべて整形済みの文字列。
// Cmd は表の group (どの command か)。Branch / Changed は cwd が repo の
// 中だったときだけ入り、外なら - で来る。
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

// meterWidth は CPU meter のマス数。
const meterWidth = 5

// CPUMeter は %cpu を固定幅の bar にする。1% でも 1 マス立てて「生きてる」を
// 見せる。100% 超 (複数 core) は満杯で頭打ち。
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

// SessionColumns は sessions view の列定義。plain と TUI で共有する。
// Cmd は列ではなく group 見出しとして出す。
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
	// meter が先、% が固定幅で後。右寄せでも meter の縦が揃う
	{"CPU", true, func(r SessionRow) string { return CPUMeter(r.CPUPct) + " " + padLeft(r.CPU, 4) }},
	{"MEM", true, func(r SessionRow) string { return r.Mem }},
}

// Quiet は沈める値。動きの無い 0 と取れなかった値。
func Quiet(v string) bool { return v == "0" || v == "-" || v == "…" || v == "" }

type palette struct {
	dim, yellow, cyan, green, red, off string
}

type column struct {
	header string
	value  func(Row) string
	tone   func(v string, p palette) string
}

// 数値列。REPO / HEAD だけが可変幅の左寄せで、あとは右寄せ。
var driftColumns = []column{
	// 手元にしか無い = 失うと戻せない
	{"CHANGED", func(r Row) string { return r.Changed }, func(_ string, p palette) string { return p.yellow }},
	{"AHEAD", func(r Row) string { return r.Ahead }, func(_ string, p palette) string { return p.yellow }},
	// remote との差 = 情報
	{"BEHIND", func(r Row) string { return r.Behind }, func(_ string, p palette) string { return p.cyan }},
	{"UNMERGED", func(r Row) string { return r.Unmerged }, func(_ string, p palette) string { return p.cyan }},
}

// Table は group ごとに見出しと列見出しを繰り返しながら drift 表を書く。
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
		// 列見出しも group ごとに繰り返して、どの行がどの列かを離れて読まなくて済むようにする
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
			// 0 と - は沈めて、動きのある数字だけ立てる (… は収集待ちの placeholder)
			tone := p.dim
			if !Quiet(v) {
				tone = c.tone(v, p)
			}
			cells[j] = paint(padLeft(v, widths[j]), tone, p.off)
		}
		fmt.Fprintf(w, "  %s  %s  %s\n", padRight(r.Repo, wrepo), padRight(r.Head, whead), strings.Join(cells, "  "))
	}
}

// SessionTable は group (command) ごとに見出しと列見出しを繰り返しながら
// session 表を書く。repos の Table と同じ文法。
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

// sessionTone は plain 出力での sessions view の色。CHANGED は drift と同じ
// 「要対応」の yellow。CPU は判定ではなく、動きの無い 0% を沈めるだけ。
func sessionTone(header string, r SessionRow, v string, p palette) string {
	switch header {
	case "CPU":
		if r.CPU == "0%" || r.CPU == "-" {
			return p.dim
		}
		return ""
	case "CHANGED":
		if Quiet(v) {
			return p.dim
		}
		return p.yellow
	default:
		if Quiet(v) {
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

// Notice は表を壊さない補足行 (dim)。
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
