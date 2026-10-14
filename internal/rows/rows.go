// Package rows reshapes collected data into display rows, shared by the
// plain output and the TUI.
package rows

import (
	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
	"github.com/gitt510/yagura/internal/render"
)

// headCap keeps the repos table's HEAD column narrow enough for a split pane.
// The branch list, one screen deeper, shows the longer name.
const headCap = 15

// Pending is the placeholder for a value that has not been collected yet.
const Pending = "…"

// Unsynced marks a value whose fetch failed, so the current remote is
// unknown. Better to say "unknown" than to dress up a stale number as fresh.
const Unsynced = "x"

// PendingInfo returns the Info shown for a row that has never been collected.
func PendingInfo() gitinfo.Info {
	return gitinfo.Info{
		Changed:    Pending,
		Head:       Pending,
		MainAhead:  Pending,
		MainBehind: Pending,
		WIP:        Pending,
		LocalOnly:  Pending,
		Gone:       Pending,
		RemoteOnly: Pending,
	}
}

// headState decides what HEAD means from the facts in Info alone.
// A repo with no baseline (no remote / not yet collected) claims nothing.
func headState(in gitinfo.Info) render.HeadState {
	switch {
	case in.Detached:
		return render.HeadDetached
	case in.Base == "":
		return render.HeadUnknown
	case in.Branch == in.Base:
		return render.HeadDefault
	default:
		return render.HeadBranch
	}
}

// Build returns drift rows in the same order as repos. infos must have the
// same length as repos.
func Build(repos []discover.Repo, infos []gitinfo.Info) []render.Row {
	out := make([]render.Row, len(repos))
	for i, r := range repos {
		out[i] = One(r, infos[i])
	}
	return out
}

// One builds the drift row for a single repo.
// For a repo whose fetch failed, the remote-derived columns become Unsynced.
// CHANGED / HEAD are local facts, so they are shown as they are.
func One(r discover.Repo, in gitinfo.Info) render.Row {
	row := render.Row{
		Group:      r.Group,
		Repo:       r.Base,
		Head:       gitinfo.Shorten(in.Head, headCap),
		HeadState:  headState(in),
		Changed:    in.Changed,
		Main:       mainLabel(in),
		WIP:        unsync(in.WIP, in.FetchFailed),
		LocalOnly:  localOnlyLabel(in),
		RemoteOnly: unsync(in.RemoteOnly, in.FetchFailed),
	}
	row.Clean = row.HeadState == render.HeadDefault && row.Changed == "0" && row.Main == render.MainSynced &&
		row.WIP == "0" && row.LocalOnly == "0" && row.RemoteOnly == "0"
	return row
}

// mainLabel puts the default branch's push and pull counts in one cell.
func mainLabel(in gitinfo.Info) string {
	switch {
	case in.MainAhead == Pending:
		return Pending
	case in.MainAhead == gitinfo.Dash || in.MainBehind == gitinfo.Dash:
		return gitinfo.Dash
	case in.FetchFailed:
		return Unsynced
	}
	return "↑" + in.MainAhead + " ↓" + in.MainBehind
}

// localOnlyLabel adds how many of the local-only branches are gone, when any.
func localOnlyLabel(in gitinfo.Info) string {
	v := unsync(in.LocalOnly, in.FetchFailed)
	if v != in.LocalOnly || in.Gone == "" || in.Gone == "0" || in.Gone == Pending || in.Gone == gitinfo.Dash {
		return v
	}
	return v + " · " + in.Gone + " gone"
}

// unsync drops a recorded value to Unsynced when the fetch failed.
// Dash is kept: it is a structural fact, that there is nothing to compare
// against in the first place.
func unsync(v string, failed bool) string {
	if !failed || v == gitinfo.Dash {
		return v
	}
	return Unsynced
}

// Branches turns one repo's branches into rows for the branch list. A failed
// fetch leaves where each branch is as last seen, but the push / pull counts
// become Unsynced.
func Branches(in gitinfo.Info) []render.BranchRow {
	out := make([]render.BranchRow, len(in.Branches))
	for i, b := range in.Branches {
		mark := "  "
		if b.Current {
			mark = "* "
		}
		where := b.Where.String()
		if b.Gone {
			where += " · gone"
		}
		out[i] = render.BranchRow{
			Name:       mark + gitinfo.ShortHead(b.Name),
			Where:      where,
			Kind:       b.Where.String(),
			Push:       unsync(b.Push, in.FetchFailed),
			Pull:       unsync(b.Pull, in.FetchFailed),
			LastCommit: orDash(b.LastCommit),
		}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return gitinfo.Dash
	}
	return s
}
