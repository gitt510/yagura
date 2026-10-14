package rows

import (
	"testing"

	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
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
