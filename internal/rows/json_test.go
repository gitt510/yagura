package rows

import (
	"testing"

	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
)

// The contract that separates the records from the table: a count that is
// not a number is null, and a failed fetch nulls the remote-derived three
// even though stale values were recorded.
func TestRecordsCounts(t *testing.T) {
	repos := []discover.Repo{
		{Path: "/r/a", Group: "~/r", Base: "a"},
		{Path: "/r/b", Group: "~/r", Base: "b"},
		{Path: "/r/c", Group: "~/r", Base: "c"},
	}
	infos := []gitinfo.Info{
		{Changed: "3", Head: "main", Branch: "main", Base: "main", Ahead: "0", Behind: "1", Unmerged: "0"},
		{Changed: "0", Head: "feat", Branch: "feat", Ahead: gitinfo.Dash, Behind: gitinfo.Dash, Unmerged: gitinfo.Dash},
		{Changed: "7", Head: "feat", Branch: "feat", Base: "main", Ahead: "2", Behind: "0", Unmerged: "5", FetchFailed: true},
	}

	got := Records(repos, infos)
	if len(got) != len(repos) {
		t.Fatalf("got %d records, want %d", len(got), len(repos))
	}

	if got[0].Path != "/r/a" || got[0].Root != "~/r" || got[0].Name != "a" {
		t.Errorf("identity = %+v", got[0])
	}
	if *got[0].Changed != 3 || *got[0].Behind != 1 || got[0].HeadState != "default" {
		t.Errorf("plain repo = %+v", got[0])
	}

	// no upstream: nothing to compare against, so null — not 0
	if got[1].Ahead != nil || got[1].Behind != nil || got[1].Unmerged != nil {
		t.Errorf("dash counts should be null: %+v", got[1])
	}
	if *got[1].Changed != 0 || got[1].HeadState != "unknown" {
		t.Errorf("no baseline = %+v", got[1])
	}

	// fetch failed: the recorded numbers are stale, so they are withheld
	if got[2].Ahead != nil || got[2].Behind != nil || got[2].Unmerged != nil {
		t.Errorf("stale counts should be null: %+v", got[2])
	}
	if !got[2].FetchFailed || *got[2].Changed != 7 || got[2].HeadState != "branch" {
		t.Errorf("fetch failed = %+v", got[2])
	}
}

// A record that has not been collected yet carries no numbers either.
func TestRecordsPending(t *testing.T) {
	got := Records([]discover.Repo{{Path: "/r/a", Base: "a"}}, []gitinfo.Info{PendingInfo()})
	r := got[0]
	if r.Changed != nil || r.Ahead != nil || r.Behind != nil || r.Unmerged != nil {
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
