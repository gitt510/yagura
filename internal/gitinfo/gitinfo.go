// Package gitinfo は repo ごとの working-tree / upstream / default-branch drift を集める。
package gitinfo

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// headMax は病的に長い branch 名で表が壊れないための表示上限。
const headMax = 32

// Dash は「該当なし / 取得不能」を表す表示値。
const Dash = "-"

// Info は 1 repo ぶんの収集結果。値は表示用の文字列で持つ。
type Info struct {
	Changed  string
	Head     string // 表示用。detached なら (short-sha)、headMax で切る
	Branch   string // detached のときは空
	Base     string // origin/HEAD の branch 名 (origin/ を落としたもの)。不明なら空
	Detached bool
	Ahead    string
	Behind   string
	Unmerged string
	// FetchFailed は fetch できず remote-tracking が古いまま collect した印。
	// fetch の成否は Collect の外で決まるので、呼び元が立てる
	FetchFailed bool
}

// FetchRepo は 1 repo を fetch する。
func FetchRepo(path string) error {
	// prune: remote branch が消えた tracking ref は 0/0 を報告し続ける
	_, err := git(path, "fetch", "--quiet", "--prune")

	// UNMERGED の基準 ref が無い repo だけ、ここで引き直す
	if _, e := git(path, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); e != nil {
		_, _ = git(path, "remote", "set-head", "origin", "--auto")
	}
	return err
}

// Fetch は各 repo を並列に fetch し、失敗した repo の path を返す。
func Fetch(paths []string, limit int) []string {
	var mu sync.Mutex
	var failed []string

	each(paths, limit, func(p string) {
		if err := FetchRepo(p); err != nil {
			mu.Lock()
			failed = append(failed, p)
			mu.Unlock()
		}
	})

	return failed
}

// CollectAll は paths と同じ順序で Info を返す。
func CollectAll(paths []string, limit int) []Info {
	infos := make([]Info, len(paths))
	idx := make(map[string]int, len(paths))
	for i, p := range paths {
		idx[p] = i
	}

	var mu sync.Mutex
	each(paths, limit, func(p string) {
		info := Collect(p)
		mu.Lock()
		infos[idx[p]] = info
		mu.Unlock()
	})
	return infos
}

// ForDirs は work tree の中にある dir だけ Collect し、dir → Info で返す。
// repo でない dir (見つからない dir を含む) は結果に載らない。
// session の cwd は repo の subdirectory のこともあるので、
// .git の有無ではなく git 自身に居場所を訊く。
func ForDirs(dirs []string, limit int) map[string]Info {
	uniq := make([]string, 0, len(dirs))
	seen := map[string]bool{}
	for _, d := range dirs {
		if d != "" && !seen[d] {
			seen[d] = true
			uniq = append(uniq, d)
		}
	}

	var mu sync.Mutex
	out := make(map[string]Info, len(uniq))
	each(uniq, limit, func(p string) {
		if !inWorkTree(p) {
			return
		}
		info := Collect(p)
		mu.Lock()
		out[p] = info
		mu.Unlock()
	})
	return out
}

func inWorkTree(path string) bool {
	out, err := git(path, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// Collect は 1 repo ぶんの drift を読む。
func Collect(path string) Info {
	info := Info{Changed: "0", Ahead: Dash, Behind: Dash, Unmerged: Dash}

	status, _ := git(path, "status", "--porcelain")
	info.Changed = strconv.Itoa(countLines(status))

	// 空 = detached。そのとき HEAD は branch ではなく commit を指す
	branch, _ := git(path, "branch", "--show-current")
	info.Branch = branch
	head := branch
	if head == "" {
		info.Detached = true
		sha, _ := git(path, "rev-parse", "--short", "HEAD")
		head = "(" + sha + ")"
	}
	info.Head = shorten(head, headMax)

	// left = behind, right = ahead。upstream を持たない HEAD は dash
	if counts, err := git(path, "rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
		if f := strings.Fields(counts); len(f) >= 2 {
			info.Behind, info.Ahead = f[0], f[1]
		}
	}

	// default branch は repo ごとに違う (main / dev / ...) ので origin/HEAD から引く
	if base, err := git(path, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && base != "" {
		info.Base = strings.TrimPrefix(base, "origin/")
		if n, err := git(path, "rev-list", "--count", base+"..HEAD"); err == nil {
			info.Unmerged = n
		}
	}

	return info
}

func git(path string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// each は limit 本までの goroutine で fn を回す。
func each(paths []string, limit int, fn func(string)) {
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for _, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(p)
		}(p)
	}
	wg.Wait()
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func shorten(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
