package rows

import (
	"testing"

	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
	"github.com/gitt510/yagura/internal/procs"
	"github.com/gitt510/yagura/internal/render"
)

// Contract on fetch failure: only the remote-derived columns turn into x;
// local facts and the structural - stay as they are.
func TestOneFetchFailed(t *testing.T) {
	repo := discover.Repo{Group: "~/g", Base: "r"}
	info := gitinfo.Info{
		Changed: "3", Head: "main", Branch: "main", Base: "main",
		MainAhead: "1", MainBehind: "2", WIP: "1", LocalOnly: "3", Gone: "2", RemoteOnly: "0",
	}

	got := One(repo, info)
	if got.Main != "↑1 ↓2" || got.WIP != "1" || got.LocalOnly != "3 · 2 gone" || got.RemoteOnly != "0" {
		t.Errorf("values changed on a successful fetch: %+v", got)
	}

	info.FetchFailed = true
	got = One(repo, info)
	if got.Main != Unsynced || got.WIP != Unsynced || got.LocalOnly != Unsynced || got.RemoteOnly != Unsynced {
		t.Errorf("remote-derived = %q, %q, %q, %q, want %q", got.Main, got.WIP, got.LocalOnly, got.RemoteOnly, Unsynced)
	}
	if got.Changed != "3" || got.Head != "main" {
		t.Errorf("local facts changed: CHANGED %q, HEAD %q", got.Changed, got.Head)
	}

	info.FetchFailed = false
	info.MainAhead, info.MainBehind = gitinfo.Dash, gitinfo.Dash
	if got := One(repo, info); got.Main != gitinfo.Dash {
		t.Errorf("MAIN without a default branch = %q, want %q", got.Main, gitinfo.Dash)
	}
}

// Clean means nothing at all to act on: on the default branch, no changes,
// the default branch in sync, and no other branch anywhere.
func TestOneClean(t *testing.T) {
	repo := discover.Repo{Group: "~/g", Base: "r"}
	info := gitinfo.Info{
		Changed: "0", Head: "main", Branch: "main", Base: "main",
		MainAhead: "0", MainBehind: "0", WIP: "0", LocalOnly: "0", Gone: "0", RemoteOnly: "0",
	}
	if !One(repo, info).Clean {
		t.Errorf("clean repo not marked clean: %+v", One(repo, info))
	}
	for name, change := range map[string]func(*gitinfo.Info){
		"changed":     func(in *gitinfo.Info) { in.Changed = "1" },
		"behind":      func(in *gitinfo.Info) { in.MainBehind = "1" },
		"wip":         func(in *gitinfo.Info) { in.WIP = "1" },
		"local-only":  func(in *gitinfo.Info) { in.LocalOnly = "1" },
		"remote-only": func(in *gitinfo.Info) { in.RemoteOnly = "1" },
		"off main":    func(in *gitinfo.Info) { in.Branch, in.Head = "feat", "feat" },
		"pending":     func(in *gitinfo.Info) { *in = PendingInfo() },
	} {
		in := info
		change(&in)
		if One(repo, in).Clean {
			t.Errorf("%s: marked clean", name)
		}
	}
}

// Branches keeps the collected order, marks the checked-out branch, spells
// gone out in WHERE, and withholds push / pull after a failed fetch.
func TestBranches(t *testing.T) {
	info := gitinfo.Info{Branches: []gitinfo.BranchInfo{
		{Name: "main", Where: gitinfo.WhereDefault, Current: true, Push: "0", Pull: "1", LastCommit: "2026-10-08"},
		{Name: "feat", Where: gitinfo.WhereLocalOnly, Gone: true, Push: gitinfo.Dash, Pull: gitinfo.Dash, LastCommit: "2026-10-01"},
		{Name: "drafts", Where: gitinfo.WhereRemoteOnly, Push: gitinfo.Dash, Pull: gitinfo.Dash},
	}}

	got := Branches(info)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Name != "* main" || got[1].Name != "  feat" {
		t.Errorf("names = %q, %q, want the checked-out branch marked", got[0].Name, got[1].Name)
	}
	if got[1].Where != "local-only · gone" || got[1].Kind != "local-only" {
		t.Errorf("gone branch = %+v", got[1])
	}
	if got[2].Where != "remote-only" || got[2].LastCommit != gitinfo.Dash {
		t.Errorf("remote-only branch = %+v", got[2])
	}
	if got[0].Pull != "1" {
		t.Errorf("pull = %q, want 1", got[0].Pull)
	}

	info.FetchFailed = true
	got = Branches(info)
	if got[0].Push != Unsynced || got[0].Pull != Unsynced || got[1].Push != gitinfo.Dash {
		t.Errorf("after a failed fetch = %+v, %+v", got[0], got[1])
	}
}

// Sessions contract: home shrinks to ~, values that could not be read become
// -, and rows are ordered by cwd.
func TestSessions(t *testing.T) {
	home := "/Users/t"
	got := Sessions([]procs.Proc{
		{PID: 300, CWD: "/Users/t/z-repo", Tmux: "work:1.2"},
		{PID: 400, CWD: "/Users/t/a-repo"},
		{PID: 500, Tmux: "work:2.1"},
		{PID: 600, CWD: "/Users/t"},
	}, home, nil)

	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	// - < ~ < ~/a-repo < ~/z-repo
	if got[0].CWD != "-" || got[0].PID != "500" {
		t.Errorf("got[0] = %+v, want cwd - の行が先頭", got[0])
	}
	if got[1].CWD != "~" {
		t.Errorf("got[1].CWD = %q, want ~ (home そのもの)", got[1].CWD)
	}
	if got[2].CWD != "~/a-repo" || got[3].CWD != "~/z-repo" {
		t.Errorf("cwd 順に並んでいない: %q, %q", got[2].CWD, got[3].CWD)
	}
	if got[3].Tmux != "work:1.2" || got[2].Tmux != "-" {
		t.Errorf("TMUX = %q, %q", got[3].Tmux, got[2].Tmux)
	}
}

// git join contract: if the cwd is in git, BRANCH / CHG are filled in;
// if not, they stay -. The key is the real path, before shortening.
func TestProcsGit(t *testing.T) {
	git := map[string]gitinfo.Info{
		"/Users/t/repo": {Changed: "2", Head: "feature", Branch: "feature", Base: "main"},
	}
	// the cwd sort puts ~/not-repo first and ~/repo second
	got := Sessions([]procs.Proc{
		{PID: 300, CWD: "/Users/t/repo"},
		{PID: 400, CWD: "/Users/t/not-repo"},
	}, "/Users/t", git)

	if got[1].Branch != "feature" || got[1].Changed != "2" {
		t.Errorf("repo の行 = %+v, want BRANCH feature / CHG 2", got[1])
	}
	if got[1].HeadState != render.HeadBranch {
		t.Errorf("HeadState = %v, want HeadBranch", got[1].HeadState)
	}
	if got[0].Branch != gitinfo.Dash || got[0].Changed != gitinfo.Dash {
		t.Errorf("repo 外の行 = %+v, want - のまま", got[0])
	}
}

// CWDs contract: empty cwds are dropped. This is the list of paths to look
// up in git.
func TestCWDs(t *testing.T) {
	got := CWDs([]procs.Proc{{CWD: "/a"}, {CWD: ""}, {CWD: "/a"}})
	if len(got) != 2 || got[0] != "/a" || got[1] != "/a" {
		t.Errorf("CWDs = %v, want [/a /a]", got)
	}
}

// elapsedLabel contract: shorten the 4 etime formats to their top two units,
// and use - when unparsable.
func TestElapsedLabel(t *testing.T) {
	cases := map[string]string{
		"00:42":       "42s",
		"05:42":       "5m",
		"01:02:03":    "1h2m",
		"10-01:02:03": "10d1h",
		"":            "-",
		"garbage":     "-",
	}
	for in, want := range cases {
		if got := elapsedLabel(in); got != want {
			t.Errorf("elapsedLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCPUAndMemLabel(t *testing.T) {
	if got := cpuLabel("12.5"); got != "13%" {
		t.Errorf("cpuLabel(12.5) = %q, want 13%%", got)
	}
	if got := cpuLabel("0.0"); got != "0%" {
		t.Errorf("cpuLabel(0.0) = %q, want 0%%", got)
	}
	if got := cpuLabel("bad"); got != "-" {
		t.Errorf("cpuLabel(bad) = %q, want -", got)
	}

	memCases := map[int]string{0: "-", 512: "512K", 40960: "40M", 2097152: "2.0G"}
	for in, want := range memCases {
		if got := memLabel(in); got != want {
			t.Errorf("memLabel(%d) = %q, want %q", in, got, want)
		}
	}
}

// fishPath contract: keep only the last element and shorten the rest to
// initials. Hidden directories keep 2 characters; ~, the root /, and the
// last element are not shortened.
func TestFishPath(t *testing.T) {
	cases := map[string]string{
		"~/ghq/github.com/gitt510/yagura": "~/g/g/g/yagura",
		"~/.config/nvim":                  "~/.c/nvim",
		"/opt/homebrew/bin":               "/o/h/bin",
		"~/dotfiles":                      "~/dotfiles",
		"~":                               "~",
		"-":                               "-",
	}
	for in, want := range cases {
		if got := fishPath(in); got != want {
			t.Errorf("fishPath(%q) = %q, want %q", in, got, want)
		}
	}
}
