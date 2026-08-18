package procs

import "testing"

// The ps contract: pick up only the declared names, and bring etime / %cpu /
// rss along. The parent table covers every process. A full-path comm still
// matches on its basename.
func TestParsePS(t *testing.T) {
	out := "  100    1 10-01:02:03   0.0  1234 /sbin/launchd\n" +
		"  200  100    01:02:03   0.0  5678 zsh\n" +
		"  300  200       02:03  12.5 40960 claude\n" +
		"  400  200    01:02:03   0.0   512 /opt/somewhere/claude\n" +
		"  500  200       00:10   0.0   100 claude-helper\n" +
		"broken line\n"

	procs, parent := parsePS(out, map[string]bool{"claude": true})
	if len(procs) != 2 {
		t.Fatalf("procs = %+v, want 2 件", procs)
	}
	want := Proc{PID: 300, Comm: "claude", Elapsed: "02:03", CPU: "12.5", RSSKB: 40960}
	if procs[0] != want {
		t.Errorf("procs[0] = %+v, want %+v", procs[0], want)
	}
	if procs[1].PID != 400 || procs[1].Elapsed != "01:02:03" {
		t.Errorf("procs[1] = %+v", procs[1])
	}
	if parent[300] != 200 || parent[200] != 100 || parent[100] != 1 {
		t.Errorf("parent = %v", parent)
	}
}

// The lsof -F pn contract: the pid on a p line pairs with the cwd on the n line that follows.
func TestParseLsof(t *testing.T) {
	out := "p300\nfcwd\nn/Users/t/repo\np400\nfcwd\nn/Users/t/other\n"

	cwds := parseLsof(out)
	if cwds[300] != "/Users/t/repo" || cwds[400] != "/Users/t/other" {
		t.Errorf("cwds = %v", cwds)
	}
}

func TestResolvePane(t *testing.T) {
	parent := map[int]int{300: 200, 200: 100, 100: 1}
	panes := map[int]string{200: "work:1.2"}

	// The immediate parent is the pane's shell
	if got := resolvePane(300, parent, panes); got != "work:1.2" {
		t.Errorf("resolvePane(300) = %q, want work:1.2", got)
	}
	// Still reached two levels up, behind a wrapper
	parent[350] = 300
	panes2 := map[int]string{100: "deep:0.0"}
	if got := resolvePane(350, parent, panes2); got != "deep:0.0" {
		t.Errorf("resolvePane(350) = %q, want deep:0.0", got)
	}
	// Outside a pane (started outside tmux) is empty
	if got := resolvePane(300, parent, map[int]string{}); got != "" {
		t.Errorf("pane 外 = %q, want empty", got)
	}
	// Terminates even for a pid missing from the parent table, or a cycle
	if got := resolvePane(999, map[int]int{999: 999}, map[int]string{}); got != "" {
		t.Errorf("循環 = %q, want empty", got)
	}
}

func TestParsePanes(t *testing.T) {
	out := "200 work:1.2\n210 work:2.1\nbroken\n"
	panes := parsePanes(out)
	if panes[200] != "work:1.2" || panes[210] != "work:2.1" || len(panes) != 2 {
		t.Errorf("panes = %v", panes)
	}
}
