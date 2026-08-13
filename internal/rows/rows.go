// Package rows は収集結果を表示行に組み替える。plain 出力と TUI で共有する。
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

// Pending は収集がまだ終わっていない値の placeholder。
const Pending = "…"

// Unsynced は fetch できず今の remote を知らない値の表示。
// 古い記録の数字を新しい顔で出すより「わからない」と言う。
const Unsynced = "x"

// PendingInfo は 1 度も収集していない行に出す Info。
func PendingInfo() gitinfo.Info {
	return gitinfo.Info{
		Changed:  Pending,
		Head:     Pending,
		Ahead:    Pending,
		Behind:   Pending,
		Unmerged: Pending,
	}
}

// headState は Info の事実だけから HEAD の意味を決める。
// 基準が引けない repo (remote 無し / 収集前) は何も主張しない。
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

// Build は repos と同じ順序で drift 行を返す。infos は repos と同じ長さであること。
func Build(repos []discover.Repo, infos []gitinfo.Info) []render.Row {
	out := make([]render.Row, len(repos))
	for i, r := range repos {
		out[i] = One(r, infos[i])
	}
	return out
}

// One は 1 repo ぶんの drift 行を組む。
// fetch に失敗した repo は remote 由来の列を Unsynced にする。
// CHANGED / HEAD は local の事実なのでそのまま出す。
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

// unsync は fetch 失敗時に記録ベースの値を Unsynced へ落とす。
// Dash は「そもそも比較対象が無い」という構造の事実なので残す。
func unsync(v string, failed bool) string {
	if !failed || v == gitinfo.Dash {
		return v
	}
	return Unsynced
}

// CWDs は session の cwd を空を除いて返す。git を引く対象の一覧になる。
func CWDs(ps []procs.Proc) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		if p.CWD != "" {
			out = append(out, p.CWD)
		}
	}
	return out
}

// Sessions は session を表示行に落とし、cwd 順に並べる。
// git は cwd (実 path) → 収集済み Info。repo でない cwd は載っていなくてよい。
// home 配下の path は ~ に縮めて出す。
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
	// group (command) → cwd → pid。CPU 順にしないのは、monitor の行が
	// refresh のたびに跳ねると追えなくなるため
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

// elapsedLabel は ps の etime ([[dd-]hh:]mm:ss) を上位 2 単位に縮める。
// 読めない形式は - にする。
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

// cpuLabel は ps の %cpu を整数 % に丸める。
func cpuLabel(cpu string) string {
	v, err := strconv.ParseFloat(cpu, 64)
	if err != nil {
		return gitinfo.Dash
	}
	return strconv.Itoa(int(v+0.5)) + "%"
}

// cpuPct は %cpu を meter / 状態判定用の整数に丸める。読めなければ 0。
func cpuPct(cpu string) int {
	v, err := strconv.ParseFloat(cpu, 64)
	if err != nil {
		return 0
	}
	return int(v + 0.5)
}

// memLabel は rss (KB) を読みやすい単位に落とす。
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

// fishPath は fish の prompt と同じ流儀で、最後の要素以外を頭文字に縮める。
// 隠し directory は . を含めて 2 文字残す。~ と先頭の / はそのまま。
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
