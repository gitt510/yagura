// Package procs は稼働中の agent CLI (claude など) の process を集める。
// 対象の process 名は呼び元が宣言する。ps で列挙し、lsof で cwd を、
// tmux の pane pid と親子関係の突き合わせで session を引く。
// どれも root 権限は要らない。
package procs

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Proc は 1 process ぶんの収集結果。取れなかった値は空 (数値は 0) で持つ。
type Proc struct {
	PID     int
	Comm    string // comm の basename。names のどれに当たったか
	CWD     string
	Tmux    string // session:window.pane。pane 配下でなければ空
	Elapsed string // ps の etime ([[dd-]hh:]mm:ss)
	CPU     string // ps の %cpu
	RSSKB   int    // resident set size (KB)
}

// List は names で宣言された稼働中の process を返す。1 つも無ければ空。
// names は comm の basename に完全一致させる。
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

	// lsof は消えた pid が混ざると非 0 で返るが、残りは出力される
	lsofOut, _ := run("lsof", "-a", "-p", joinPIDs(procs), "-d", "cwd", "-F", "pn")
	cwds := parseLsof(lsofOut)

	// tmux が無い / server が居ない場合は全行 pane なし扱いになるだけ
	tmuxOut, _ := run("tmux", "list-panes", "-a", "-F", "#{pane_pid} #{session_name}:#{window_index}.#{pane_index}")
	panes := parsePanes(tmuxOut)

	for i := range procs {
		procs[i].CWD = cwds[procs[i].PID]
		procs[i].Tmux = resolvePane(procs[i].PID, parent, panes)
	}
	return procs
}

// parsePS は ps の出力から監視対象の行と、全 process の親子表を取る。
// 親子表は対象以外も含む。pane の shell まで辿るのに使うため。
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
		// comm は環境によって full path のこともあり、space も入り得る
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

// parseLsof は `lsof -F pn` の出力を pid → cwd に落とす。
// p 行が pid、続く n 行がその cwd。
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

// parsePanes は tmux list-panes の出力を pane の shell pid → 表示名に落とす。
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

// resolvePane は pid から親を辿り、最初に当たった pane の表示名を返す。
// claude は普通 pane の shell の直下だが、wrapper を挟んでも拾えるように歩く。
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
