// The parts specific to the sessions view. The collection cmd and the table
// assembly are confined to this file, leaving ui.go with only the view wiring.
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

// sessionsCmd collects the running sessions in one pass. What to watch comes
// from the config. Each session's repo is read for local facts only, no fetch.
func sessionsCmd(gen int, commands []string) tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		ps := procs.List(commands)
		git := gitinfo.ForDirs(rows.CWDs(ps), concurrency)
		return sessionsResultMsg{gen: gen, list: rows.Sessions(ps, home, git)}
	}
}

// buildSessionTable stacks an independent bordered table per command. It uses
// the same grammar as the repos buildTable, with the group heading annotating
// which command it is.
// Column widths are shared across all groups, so the column positions do not
// move as the cursor crosses from one table to the next.
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

// sessionStyle is the cell coloring for the sessions view. BRANCH carries the
// same meaning as HEAD in drift, CHANGED is the "needs action" yellow. CPU is
// not a judgement; it only sinks an idle 0%.
func sessionStyle(header string, r render.SessionRow, v string, th theme) lipgloss.Style {
	switch header {
	case "CPU":
		if r.CPU == gitinfo.Dash {
			return th.dim
		}
		return lipgloss.NewStyle()
	case "BRANCH":
		if render.Absent(v) {
			return th.dim
		}
		return headTone(r.HeadState, th)
	case "CHANGED":
		if render.Absent(v) || v == "0" {
			return th.dim
		}
		return th.local
	default:
		if render.Absent(v) {
			return th.dim
		}
		return lipgloss.NewStyle()
	}
}
