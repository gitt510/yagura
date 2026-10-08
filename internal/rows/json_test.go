package rows

import (
	"testing"

	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
)

// The contract that separates the records from the table: a count that is
// not a number is null, and a failed fetch nulls the remote-derived counts
// even though stale values were recorded.
func TestRecordsCounts(t *testing.T) {
	repos := []discover.Repo{
		{Path: "/r/a", Group: "~/r", Base: "a"},
		{Path: "/r/b", Group: "~/r", Base: "b"},
		{Path: "/r/c", Group: "~/r", Base: "c"},
	}
	feat := []gitinfo.BranchInfo{{Name: "feat", Where: gitinfo.WhereWIP, Current: true, Push: "2", Pull: "0", LastCommit: "2026-10-08"}}
	infos := []gitinfo.Info{
		{Changed: "3", Head: "main", Branch: "main", Base: "main", MainAhead: "0", MainBehind: "1", WIP: "0", LocalOnly: "2", Gone: "1", RemoteOnly: "0"},
		{Changed: "0", Head: "feat", Branch: "feat", MainAhead: gitinfo.Dash, MainBehind: gitinfo.Dash, WIP: gitinfo.Dash, LocalOnly: gitinfo.Dash, Gone: gitinfo.Dash, RemoteOnly: gitinfo.Dash},
		{Changed: "7", Head: "feat", Branch: "feat", Base: "main", MainAhead: "0", MainBehind: "0", WIP: "1", LocalOnly: "0", Gone: "0", RemoteOnly: "0", Branches: feat, FetchFailed: true},
	}

	got := Records(repos, infos)
	if len(got) != len(repos) {
		t.Fatalf("got %d records, want %d", len(got), len(repos))
	}

	if got[0].Path != "/r/a" || got[0].Root != "~/r" || got[0].Name != "a" {
		t.Errorf("identity = %+v", got[0])
	}
	if *got[0].Changed != 3 || *got[0].MainBehind != 1 || *got[0].LocalOnly != 2 || *got[0].Gone != 1 || got[0].HeadState != "default" {
		t.Errorf("plain repo = %+v", got[0])
	}
	if got[0].Branches == nil || len(got[0].Branches) != 0 {
		t.Errorf("no branches should be an empty list: %+v", got[0].Branches)
	}

	// no default branch: nothing to compare against, so null — not 0
	if got[1].MainAhead != nil || got[1].MainBehind != nil || got[1].WIP != nil {
		t.Errorf("dash counts should be null: %+v", got[1])
	}
	if *got[1].Changed != 0 || got[1].HeadState != "unknown" {
		t.Errorf("no baseline = %+v", got[1])
	}

	// fetch failed: the recorded numbers are stale, so they are withheld
	if got[2].MainAhead != nil || got[2].WIP != nil || got[2].RemoteOnly != nil {
		t.Errorf("stale counts should be null: %+v", got[2])
	}
	if !got[2].FetchFailed || *got[2].Changed != 7 || got[2].HeadState != "branch" {
		t.Errorf("fetch failed = %+v", got[2])
	}
	if b := got[2].Branches; len(b) != 1 || b[0].Where != "wip" || !b[0].Current || b[0].Push != nil || *b[0].LastCommit != "2026-10-08" {
		t.Errorf("branch record = %+v", b)
	}
}

// A record that has not been collected yet carries no numbers either.
func TestRecordsPending(t *testing.T) {
	got := Records([]discover.Repo{{Path: "/r/a", Base: "a"}}, []gitinfo.Info{PendingInfo()})
	r := got[0]
	if r.Changed != nil || r.MainAhead != nil || r.WIP != nil || r.LocalOnly != nil || r.RemoteOnly != nil {
		t.Errorf("pending counts should be null: %+v", r)
	}
}

func TestRecordsDetached(t *testing.T) {
	got := Records(
		[]discover.Repo{{Path: "/r/a", Base: "a"}},
		[]gitinfo.Info{{Changed: "0", Head: "(abc1234)", Detached: true, Base: "main"}},
	)
	if got[0].HeadState != "detached" {
		t.Errorf("head_state = %q, want detached", got[0].HeadState)
	}
}
