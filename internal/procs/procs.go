// Package procs collects the processes of running agent CLIs (claude and
// friends). The caller declares which process names to watch. It lists them
// with ps, reads cwd with lsof, and resolves the session by matching tmux
// pane pids against the parent chain. None of it needs root.
package procs

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Proc is what was collected for a single process. Values that could not be
// read are held empty (0 for numbers).
type Proc struct {
	PID     int
	Comm    string // basename of comm; which of names it matched
	CWD     string
	Tmux    string // session:window.pane; empty if not under a pane
	Elapsed string // etime from ps ([[dd-]hh:]mm:ss)
	CPU     string // %cpu from ps
	RSSKB   int    // resident set size (KB)
}

// List returns the running processes declared in names, or nothing if none
// are running. names are matched exactly against the basename of comm.
func List(names []string) []Proc {
	watch := make(map[string]bool, len(names))
	for _, n := range names {
		watch[n] = true
	}

	psOut, err := run("ps", "-axo", "pid=,ppid=,etime=,%cpu=,rss=,comm=")
	if err != nil {
		return nil
	}
	procs, parent := parsePS(psOut, watch)
	if len(procs) == 0 {
		return nil
	}

	// lsof exits non-zero if a pid has since died, but still prints the rest
	lsofOut, _ := run("lsof", "-a", "-p", joinPIDs(procs), "-d", "cwd", "-F", "pn")
	cwds := parseLsof(lsofOut)

	// Without tmux, or with no server running, every row simply has no pane
	tmuxOut, _ := run("tmux", "list-panes", "-a", "-F", "#{pane_pid} #{session_name}:#{window_index}.#{pane_index}")
	panes := parsePanes(tmuxOut)

	for i := range procs {
		procs[i].CWD = cwds[procs[i].PID]
		procs[i].Tmux = resolvePane(procs[i].PID, parent, panes)
	}
	return procs
}

// parsePS takes the watched rows out of the ps output along with a parent
// table for every process. The table covers processes that are not watched
// too, since it is used to walk up to the pane's shell.
func parsePS(out string, watch map[string]bool) (procs []Proc, parent map[int]int) {
	parent = map[int]int{}
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		parent[pid] = ppid
		// comm may be a full path depending on the environment, and may hold spaces
		comm := strings.Join(f[5:], " ")
		base := filepath.Base(comm)
		if !watch[base] {
			continue
		}
		rss, _ := strconv.Atoi(f[4])
		procs = append(procs, Proc{PID: pid, Comm: base, Elapsed: f[2], CPU: f[3], RSSKB: rss})
	}
	return procs, parent
}

// parseLsof reduces `lsof -F pn` output to pid → cwd.
// A p line is the pid, and the n line that follows is its cwd.
func parseLsof(out string) map[int]string {
	cwds := map[int]string{}
	pid := 0
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n") && pid != 0:
			cwds[pid] = line[1:]
		}
	}
	return cwds
}

// parsePanes reduces tmux list-panes output to pane shell pid → display name.
func parsePanes(out string) map[int]string {
	panes := map[int]string{}
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		if pid, err := strconv.Atoi(f[0]); err == nil {
			panes[pid] = f[1]
		}
	}
	return panes
}

// resolvePane walks up from pid and returns the display name of the first
// pane it hits. claude usually sits directly under the pane's shell, but the
// walk keeps it findable behind a wrapper.
func resolvePane(pid int, parent map[int]int, panes map[int]string) string {
	for seen := map[int]bool{}; pid > 1 && !seen[pid]; {
		seen[pid] = true
		if name, ok := panes[pid]; ok {
			return name
		}
		pid = parent[pid]
	}
	return ""
}

func joinPIDs(procs []Proc) string {
	s := make([]string, len(procs))
	for i, p := range procs {
		s[i] = strconv.Itoa(p.PID)
	}
	return strings.Join(s, ",")
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}
