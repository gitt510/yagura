package gitinfo

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// run executes git in dir and fails the test on error.
func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// Collect contract on a real origin: every branch other than the default
// lands in exactly one of WIP / LOCAL-ONLY / REMOTE-ONLY by where it exists,
// a branch whose upstream was deleted is gone, and push / pull compare a
// branch with origin/<same name>.
func TestCollectBranches(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")
	other := filepath.Join(root, "other")

	run(t, root, "init", "-q", "--bare", "-b", "main", origin)
	run(t, root, "clone", "-q", origin, work)
	run(t, work, "commit", "-q", "--allow-empty", "-m", "init")
	run(t, work, "push", "-q", "-u", "origin", "main")
	run(t, work, "remote", "set-head", "origin", "main")

	// wip: pushed, then one more local commit
	run(t, work, "switch", "-q", "-c", "wip")
	run(t, work, "push", "-q", "-u", "origin", "wip")
	run(t, work, "commit", "-q", "--allow-empty", "-m", "wip")
	// never pushed
	run(t, work, "switch", "-q", "-c", "local", "main")
	// gone: pushed, then deleted on the remote
	run(t, work, "switch", "-q", "-c", "merged", "main")
	run(t, work, "push", "-q", "-u", "origin", "merged")
	run(t, work, "push", "-q", "origin", "--delete", "merged")
	// remote-only: pushed from another clone
	run(t, root, "clone", "-q", origin, other)
	run(t, other, "switch", "-q", "-c", "elsewhere")
	run(t, other, "push", "-q", "origin", "elsewhere")
	// main falls behind origin by one
	run(t, other, "switch", "-q", "main")
	run(t, other, "commit", "-q", "--allow-empty", "-m", "ahead")
	run(t, other, "push", "-q", "origin", "main")

	run(t, work, "switch", "-q", "wip")
	if err := FetchRepo(work); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	in := Collect(work)

	if in.Base != "main" || in.MainAhead != "0" || in.MainBehind != "1" {
		t.Errorf("main = base %q, ↑%s ↓%s, want main ↑0 ↓1", in.Base, in.MainAhead, in.MainBehind)
	}
	if in.WIP != "1" || in.LocalOnly != "2" || in.Gone != "1" || in.RemoteOnly != "1" {
		t.Errorf("counts = wip %s, local-only %s (gone %s), remote-only %s, want 1, 2 (1), 1", in.WIP, in.LocalOnly, in.Gone, in.RemoteOnly)
	}

	want := []struct {
		name    string
		where   Where
		gone    bool
		current bool
		push    string
	}{
		{"main", WhereDefault, false, false, "0"},
		{"elsewhere", WhereRemoteOnly, false, false, Dash},
		{"local", WhereLocalOnly, false, false, Dash},
		{"merged", WhereLocalOnly, true, false, Dash},
		{"wip", WhereWIP, false, true, "1"},
	}
	if len(in.Branches) != len(want) {
		t.Fatalf("branches = %+v, want %d", in.Branches, len(want))
	}
	for i, w := range want {
		b := in.Branches[i]
		if b.Name != w.name || b.Where != w.where || b.Gone != w.gone || b.Current != w.current || b.Push != w.push {
			t.Errorf("branch %d = %+v, want %+v", i, b, w)
		}
		if b.LastCommit == "" {
			t.Errorf("branch %s has no last commit date", b.Name)
		}
	}
}
