package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
)

// The auto-refresh interval is per view. Running repos at the procs rate would
// fire git fetch across every repo far too often.
func TestIntervalPerView(t *testing.T) {
	m := newModel(Options{SessionsInterval: 10 * time.Second, ReposInterval: time.Minute})
	if got := m.interval(); got != time.Minute {
		t.Errorf("repos の interval = %s, want 1m", got)
	}
	m.view = viewSessions
	if got := m.interval(); got != 10*time.Second {
		t.Errorf("sessions の interval = %s, want 10s", got)
	}
}

// Dropping zero units could turn "10s" into "1", so pin down the boundaries.
func TestFormatInterval(t *testing.T) {
	cases := map[string]string{
		"1m":      "1m",
		"1h":      "1h",
		"3600s":   "1h",
		"1m30s":   "1m30s",
		"1h30m":   "1h30m",
		"90m":     "1h30m",
		"1h0m30s": "1h0m30s",
		"10s":     "10s",
		"100s":    "1m40s",
		"20m":     "20m",
		"5s":      "5s",
	}
	for in, want := range cases {
		d, err := time.ParseDuration(in)
		if err != nil {
			t.Fatalf("ParseDuration(%q): %v", in, err)
		}
		if got := formatInterval(d); got != want {
			t.Errorf("formatInterval(%s) = %q, want %q", d, got, want)
		}
	}
}

// The contract of spliceLine: w cells starting at x are replaced by box, and a
// short base has the gap filled with spaces. This is what keeps the columns
// aligned when a float lands in the middle of a bordered line.
func TestSpliceLine(t *testing.T) {
	if got := spliceLine("abcdefghij", "XXX", 3, 3); got != "abcXXXghij" {
		t.Errorf("spliceLine = %q, want abcXXXghij", got)
	}
	if got := spliceLine("ab", "XXX", 4, 3); got != "ab  XXX" {
		t.Errorf("短い base = %q, want ab  XXX", got)
	}
	if got := spliceLine("", "XXX", 0, 3); got != "XXX" {
		t.Errorf("空 base = %q, want XXX", got)
	}
}

// The contract of gradientLine: painting in segments never drops a character.
func TestGradientLine(t *testing.T) {
	plain := []lipgloss.Style{{}, {}, {}}
	if got := gradientLine("abcdefg", plain); got != "abcdefg" {
		t.Errorf("gradientLine = %q, want abcdefg", got)
	}
}

// enter opens the focused repo's branch list and esc steps back to the repos
// table; only on the repos table does esc quit.
func TestBranchesNavigation(t *testing.T) {
	m := newModel(Options{Repos: []discover.Repo{{Group: "~/g", Base: "a"}, {Group: "~/g", Base: "b"}}})
	m.width, m.height = 100, 40
	m.infos[1] = gitinfo.Info{Changed: "0", Head: "main", Branch: "main", Base: "main",
		Branches: []gitinfo.BranchInfo{{Name: "main", Where: gitinfo.WhereDefault, Current: true, Push: "0", Pull: "0"}}}
	m.rebuild()

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.inBranches() || m.open != 1 {
		t.Fatalf("enter: open = %d, want the focused repo 1", m.open)
	}
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil || m.inBranches() {
		t.Errorf("esc in the branch list should step back, not quit")
	}
	if _, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}); cmd == nil {
		t.Errorf("esc on the repos table should quit")
	}
}
