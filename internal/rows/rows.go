// Package rows reshapes collected data into display rows, shared by the
// plain output and the TUI.
package rows

import (
	"sort"
	"strconv"
	"strings"

	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
	"github.com/gitt510/yagura/internal/procs"
	"github.com/gitt510/yagura/internal/render"
)

// Pending is the placeholder for a value that has not been collected yet.
const Pending = "…"

// Unsynced marks a value whose fetch failed, so the current remote is
// unknown. Better to say "unknown" than to dress up a stale number as fresh.
const Unsynced = "x"

// PendingInfo returns the Info shown for a row that has never been collected.
func PendingInfo() gitinfo.Info {
	return gitinfo.Info{
		Changed:  Pending,
		Head:     Pending,
		Ahead:    Pending,
		Behind:   Pending,
		Unmerged: Pending,
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
	return render.Row{
		Group:     r.Group,
		Repo:      r.Base,
		Head:      in.Head,
		HeadState: headState(in),
		Changed:   in.Changed,
		Ahead:     unsync(in.Ahead, in.FetchFailed),
		Behind:    unsync(in.Behind, in.FetchFailed),
		Unmerged:  unsync(in.Unmerged, in.FetchFailed),
	}
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

// BranchView flattens each repo into one row per local branch, for the TUI's
// branch mode. The default branch always leads, so it holds the same slot in
// every repo block; the rest follow by name. The block's first row carries
// the REPO name and is the only focusable one (Sub false), keeping repo and
// focusable row 1:1. The checked-out branch is the only row with a working
// tree (CHANGED); its number against the others' dash tells them apart, so
// no marker is needed. A repo whose branches have not been collected yet
// carries a trailing pending row, so its absence is never mistaken for "no
// other branches". branches is keyed by repo index; a present key means
// collected, even when empty.
func BranchView(repos []discover.Repo, infos []gitinfo.Info, branches map[int][]gitinfo.BranchInfo) []render.Row {
	type entry struct {
		name    string
		def     bool
		current bool
		row     render.Row
	}

	var out []render.Row
	for i, r := range repos {
		in := infos[i]
		list, loaded := branches[i]

		h := One(r, in)
		es := make([]entry, 0, len(list)+2)
		es = append(es, entry{name: h.Head, def: h.HeadState == render.HeadDefault, current: true, row: h})
		for _, b := range list {
			state := render.HeadBranch
			if b.Name == in.Base {
				state = render.HeadDefault
			}
			es = append(es, entry{name: gitinfo.ShortHead(b.Name), def: state == render.HeadDefault, row: render.Row{
				Group:     r.Group,
				HeadState: state,
				Changed:   gitinfo.Dash,
				Ahead:     unsync(b.Ahead, in.FetchFailed),
				Behind:    unsync(b.Behind, in.FetchFailed),
				Unmerged:  unsync(b.Unmerged, in.FetchFailed),
			}})
		}
		sort.SliceStable(es, func(a, b int) bool {
			if es[a].def != es[b].def {
				return es[a].def
			}
			return es[a].name < es[b].name
		})
		if !loaded {
			es = append(es, entry{name: Pending, row: render.Row{
				Group: r.Group, Changed: Pending, Ahead: Pending, Behind: Pending, Unmerged: Pending,
			}})
		}

		for j, e := range es {
			row := e.row
			row.Head = e.name
			row.Repo, row.Sub = "", true
			if j == 0 {
				row.Repo, row.Sub = r.Base, false
			}
			out = append(out, row)
		}
	}
	return out
}

// CWDs returns the sessions' cwds, dropping empty ones. This is the list of
// paths to look up in git.
func CWDs(ps []procs.Proc) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		if p.CWD != "" {
			out = append(out, p.CWD)
		}
	}
	return out
}

// Sessions turns sessions into display rows, ordered by cwd.
// git maps cwd (the real path) to collected Info; a cwd that is not a repo
// need not appear. Paths under home are shortened to ~.
func Sessions(ps []procs.Proc, home string, git map[string]gitinfo.Info) []render.SessionRow {
	out := make([]render.SessionRow, len(ps))
	for i, p := range ps {
		row := render.SessionRow{
			Cmd:     orDash(p.Comm),
			CWD:     fishPath(tilde(orDash(p.CWD), home)),
			Branch:  gitinfo.Dash,
			Changed: gitinfo.Dash,
			Tmux:    orDash(p.Tmux),
			PID:     strconv.Itoa(p.PID),
			Elapsed: elapsedLabel(p.Elapsed),
			CPU:     cpuLabel(p.CPU),
			CPUPct:  cpuPct(p.CPU),
			Mem:     memLabel(p.RSSKB),
		}
		if in, ok := git[p.CWD]; ok {
			row.Branch = in.Head
			row.HeadState = headState(in)
			row.Changed = in.Changed
		}
		out[i] = row
	}
	// group (command) → cwd → pid. Not sorted by CPU because rows that jump
	// around on every refresh are impossible to follow in a monitor
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cmd != out[j].Cmd {
			return out[i].Cmd < out[j].Cmd
		}
		if out[i].CWD != out[j].CWD {
			return out[i].CWD < out[j].CWD
		}
		return out[i].PID < out[j].PID
	})
	return out
}

// elapsedLabel shortens ps etime ([[dd-]hh:]mm:ss) to its top two units.
// An unparsable format becomes -.
func elapsedLabel(etime string) string {
	days := 0
	rest := etime
	if i := strings.Index(rest, "-"); i >= 0 {
		d, err := strconv.Atoi(rest[:i])
		if err != nil {
			return gitinfo.Dash
		}
		days, rest = d, rest[i+1:]
	}

	parts := strings.Split(rest, ":")
	nums := make([]int, len(parts))
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil {
			return gitinfo.Dash
		}
		nums[i] = n
	}

	var h, m, s int
	switch len(nums) {
	case 2:
		m, s = nums[0], nums[1]
	case 3:
		h, m, s = nums[0], nums[1], nums[2]
	default:
		return gitinfo.Dash
	}

	switch {
	case days > 0:
		return strconv.Itoa(days) + "d" + strconv.Itoa(h) + "h"
	case h > 0:
		return strconv.Itoa(h) + "h" + strconv.Itoa(m) + "m"
	case m > 0:
		return strconv.Itoa(m) + "m"
	default:
		return strconv.Itoa(s) + "s"
	}
}

// cpuLabel rounds ps %cpu to an integer percentage.
func cpuLabel(cpu string) string {
	v, err := strconv.ParseFloat(cpu, 64)
	if err != nil {
		return gitinfo.Dash
	}
	return strconv.Itoa(int(v+0.5)) + "%"
}

// cpuPct rounds %cpu to an integer for the meter and state checks. 0 if
// unparsable.
func cpuPct(cpu string) int {
	v, err := strconv.ParseFloat(cpu, 64)
	if err != nil {
		return 0
	}
	return int(v + 0.5)
}

// memLabel reduces rss (KB) to a readable unit.
func memLabel(kb int) string {
	switch {
	case kb <= 0:
		return gitinfo.Dash
	case kb >= 1<<20:
		return strconv.FormatFloat(float64(kb)/(1<<20), 'f', 1, 64) + "G"
	case kb >= 1<<10:
		return strconv.Itoa(kb>>10) + "M"
	default:
		return strconv.Itoa(kb) + "K"
	}
}

func orDash(s string) string {
	if s == "" {
		return gitinfo.Dash
	}
	return s
}

// fishPath shortens every element but the last to its initial, the way the
// fish prompt does. A hidden directory keeps 2 characters including the .
// ~ and a leading / are left alone.
func fishPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts[:max(0, len(parts)-1)] {
		r := []rune(p)
		n := 1
		if strings.HasPrefix(p, ".") {
			n = 2
		}
		if p != "~" && len(r) > n {
			parts[i] = string(r[:n])
		}
	}
	return strings.Join(parts, "/")
}

func tilde(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+"/"); ok {
		return "~/" + rest
	}
	return path
}
