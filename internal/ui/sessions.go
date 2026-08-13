// sessions view の固有部分。収集 cmd と表の組み立てをこの file に閉じ、
// ui.go 側は view の配線だけ持つ。
package ui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gitt510/yagura/internal/gitinfo"
	"github.com/gitt510/yagura/internal/procs"
	"github.com/gitt510/yagura/internal/render"
	"github.com/gitt510/yagura/internal/rows"
)

type sessionsResultMsg struct {
	gen  int
	list []render.SessionRow
}

// sessionsCmd は稼働中の session を 1 回で引く。監視対象は config 由来。
// 各 session の repo は fetch せず local の事実だけ読む。
func sessionsCmd(gen int, commands []string) tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		ps := procs.List(commands)
		git := gitinfo.ForDirs(rows.CWDs(ps), concurrency)
		return sessionsResultMsg{gen: gen, list: rows.Sessions(ps, home, git)}
	}
}

// buildSessionTable は command ごとに独立した枠付きの表を積む。repos の
// buildTable と同じ文法で、group 見出しが「どの command か」の annotation。
// 列幅は全 group で共通にして、cursor が表を渡っても列位置が動かないようにする。
func buildSessionTable(rs []render.SessionRow, th theme) table {
	widths := make([]int, len(render.SessionColumns))
	headers := make([]string, len(render.SessionColumns))
	rights := make([]bool, len(render.SessionColumns))
	for i, c := range render.SessionColumns {
		headers[i], rights[i] = c.Header, c.Right
		widths[i] = lipgloss.Width(c.Header)
		for _, r := range rs {
			if n := lipgloss.Width(c.Value(r)); n > widths[i] {
				widths[i] = n
			}
		}
	}

	counts := map[string]int{}
	for _, r := range rs {
		counts[r.Cmd]++
	}

	t := table{widths: widths, th: th}
	group := ""
	for i, r := range rs {
		if r.Cmd != group {
			if group != "" {
				t.push(group, t.bottom())
			}
			group = r.Cmd
			t.push(group, "")
			t.push(group, th.group.Render(group)+th.groupCount.Render(" · "+countLabel(counts[group], "session")))
			t.push(group, t.top())
			t.push(group, t.headerRow(headers, rights))
			t.push(group, t.rule())
		}

		cells := make([]cell, len(render.SessionColumns))
		for j, c := range render.SessionColumns {
			v := c.Value(r)
			cells[j] = cell{text: pad(v, widths[j], c.Right), style: sessionStyle(c.Header, r, v, th)}
		}
		t.lines = append(t.lines, tableLine{kind: lineRepo, group: group, cells: cells, ref: i})
	}
	if group != "" {
		t.push(group, t.bottom())
	}
	return t
}

// sessionStyle は sessions view の cell の色。BRANCH は drift の HEAD と
// 同じ意味づけ、CHANGED は「要対応」の yellow。CPU は判定ではなく、
// 動きの無い 0% を沈めるだけ。
func sessionStyle(header string, r render.SessionRow, v string, th theme) lipgloss.Style {
	switch header {
	case "CPU":
		if r.CPU == "0%" || r.CPU == gitinfo.Dash {
			return th.dim
		}
		return lipgloss.NewStyle()
	case "BRANCH":
		if render.Quiet(v) {
			return th.dim
		}
		return headTone(r.HeadState, th)
	case "CHANGED":
		if render.Quiet(v) {
			return th.dim
		}
		return th.local
	default:
		if render.Quiet(v) {
			return th.dim
		}
		return lipgloss.NewStyle()
	}
}
