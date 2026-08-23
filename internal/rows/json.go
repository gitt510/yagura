package rows

import (
	"sort"
	"strconv"

	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
	"github.com/gitt510/yagura/internal/procs"
	"github.com/gitt510/yagura/internal/render"
)

// Records returns one drift record per repo, in the same order as repos.
// infos must have the same length as repos. Unlike Build, the counts stay
// numbers: a value that could not be compared is null, so nothing has to be
// read back out of a display string.
func Records(repos []discover.Repo, infos []gitinfo.Info) []render.RepoRecord {
	out := make([]render.RepoRecord, len(repos))
	for i, r := range repos {
		in := infos[i]
		rec := render.RepoRecord{
			Root:        r.Group,
			Name:        r.Base,
			Path:        r.Path,
			Head:        in.Head,
			HeadState:   headState(in).String(),
			Changed:     num(in.Changed),
			Ahead:       num(in.Ahead),
			Behind:      num(in.Behind),
			Unmerged:    num(in.Unmerged),
			FetchFailed: in.FetchFailed,
		}
		// a stale remote-tracking ref is not an answer; say so with null
		if in.FetchFailed {
			rec.Ahead, rec.Behind, rec.Unmerged = nil, nil, nil
		}
		out[i] = rec
	}
	return out
}

// SessionRecords returns one record per session, ordered the way the table
// orders them. git maps cwd to collected Info; a cwd outside a work tree
// need not appear. Paths are absolute here — this is the machine's copy.
func SessionRecords(ps []procs.Proc, git map[string]gitinfo.Info) []render.SessionRecord {
	out := make([]render.SessionRecord, len(ps))
	for i, p := range ps {
		rec := render.SessionRecord{
			Command:   p.Comm,
			PID:       p.PID,
			CWD:       p.CWD,
			HeadState: render.HeadUnknown.String(),
			Tmux:      str(p.Tmux),
			Elapsed:   elapsedLabel(p.Elapsed),
			CPUPct:    cpuFloat(p.CPU),
			RSSKB:     p.RSSKB,
		}
		if in, ok := git[p.CWD]; ok {
			rec.Branch = str(in.Head)
			rec.HeadState = headState(in).String()
			rec.Changed = num(in.Changed)
		}
		out[i] = rec
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Command != out[j].Command {
			return out[i].Command < out[j].Command
		}
		if out[i].CWD != out[j].CWD {
			return out[i].CWD < out[j].CWD
		}
		return out[i].PID < out[j].PID
	})
	return out
}

// num reads a collected count. Dash (nothing to compare against) and Pending
// (not collected) are not numbers, so they become null.
func num(v string) *int {
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}

func str(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func cpuFloat(cpu string) *float64 {
	v, err := strconv.ParseFloat(cpu, 64)
	if err != nil {
		return nil
	}
	return &v
}
