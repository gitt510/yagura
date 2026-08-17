// Package gitinfo collects per-repo working-tree / upstream / default-branch drift.
package gitinfo

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// headMax is the display cap that keeps pathologically long branch names
// from breaking the table.
const headMax = 32

// Dash is the display value for "not applicable / could not be read".
const Dash = "-"

// Info is what was collected for one repo. Values are held as display strings.
type Info struct {
	Changed  string
	Head     string // for display; (short-sha) when detached, cut at headMax
	Branch   string // empty when detached
	Base     string // origin/HEAD's branch name (origin/ stripped); empty if unknown
	Detached bool
	Ahead    string
	Behind   string
	Unmerged string
	// FetchFailed marks that fetch failed and collection ran against a stale
	// remote-tracking ref. Whether fetch succeeded is decided outside
	// Collect, so the caller sets this
	FetchFailed bool
}

// FetchRepo fetches a single repo.
func FetchRepo(path string) error {
	// prune: a tracking ref whose remote branch is gone keeps reporting 0/0
	_, err := git(path, "fetch", "--quiet", "--prune")

	// re-resolve here only for repos with no reference ref for UNMERGED
	if _, e := git(path, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); e != nil {
		_, _ = git(path, "remote", "set-head", "origin", "--auto")
	}
	return err
}

// Fetch fetches each repo in parallel and returns the paths that failed.
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

// CollectAll returns Info in the same order as paths.
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

// ForDirs collects only the dirs that sit inside a work tree and returns
// dir -> Info. Dirs that aren't repos (including missing ones) are omitted.
// A session's cwd can be a subdirectory of the repo, so ask git itself
// where it is rather than looking for .git.
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

// Collect reads the drift for a single repo.
func Collect(path string) Info {
	info := Info{Changed: "0", Ahead: Dash, Behind: Dash, Unmerged: Dash}

	status, _ := git(path, "status", "--porcelain")
	info.Changed = strconv.Itoa(countLines(status))

	// empty = detached; HEAD then points at a commit, not a branch
	branch, _ := git(path, "branch", "--show-current")
	info.Branch = branch
	head := branch
	if head == "" {
		info.Detached = true
		sha, _ := git(path, "rev-parse", "--short", "HEAD")
		head = "(" + sha + ")"
	}
	info.Head = shorten(head, headMax)

	// left = behind, right = ahead; a HEAD with no upstream stays dash
	if counts, err := git(path, "rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
		if f := strings.Fields(counts); len(f) >= 2 {
			info.Behind, info.Ahead = f[0], f[1]
		}
	}

	// the default branch differs per repo (main / dev / ...), so read it from origin/HEAD
	if base, err := git(path, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && base != "" {
		info.Base = strings.TrimPrefix(base, "origin/")
		if n, err := git(path, "rev-list", "--count", base+"..HEAD"); err == nil {
			info.Unmerged = n
		}
	}

	return info
}

// BranchInfo is one local branch's drift: the same columns as Info minus the
// working tree, which belongs to HEAD only.
type BranchInfo struct {
	Name     string
	Ahead    string
	Behind   string
	Unmerged string
}

// Branches lists the local branches other than the checked-out one, with the
// same drift reads as Collect. It never fetches; the tracking refs are read
// as they are.
func Branches(path string) []BranchInfo {
	refs, err := git(path, "for-each-ref", "refs/heads", "--format=%(HEAD)\t%(refname:short)")
	if err != nil || refs == "" {
		return nil
	}
	base, _ := git(path, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")

	var out []BranchInfo
	for _, line := range strings.Split(refs, "\n") {
		marker, name, ok := strings.Cut(line, "\t")
		if !ok || marker == "*" || name == "" {
			continue
		}
		b := BranchInfo{Name: name, Ahead: Dash, Behind: Dash, Unmerged: Dash}
		if counts, err := git(path, "rev-list", "--left-right", "--count", name+"@{upstream}..."+name); err == nil {
			if f := strings.Fields(counts); len(f) >= 2 {
				b.Behind, b.Ahead = f[0], f[1]
			}
		}
		if base != "" {
			if n, err := git(path, "rev-list", "--count", base+".."+name); err == nil {
				b.Unmerged = n
			}
		}
		out = append(out, b)
	}
	return out
}

// ShortHead caps a branch name for display, the same cut Collect applies to
// HEAD.
func ShortHead(s string) string { return shorten(s, headMax) }

func git(path string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// each runs fn across at most limit goroutines.
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
