// Package gitinfo collects per-repo working-tree drift, the default branch
// against origin, and where every other branch lives.
package gitinfo

import (
	"bytes"
	"os/exec"
	"sort"
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
	// MainAhead / MainBehind compare the local default branch with its
	// origin counterpart: commits not pushed / not pulled
	MainAhead  string
	MainBehind string
	// Every branch other than the default falls into exactly one of these,
	// by where it exists. Gone is the part of LocalOnly whose upstream was
	// deleted on the remote
	WIP        string
	LocalOnly  string
	Gone       string
	RemoteOnly string
	// Branches lists every branch, the default one first
	Branches []BranchInfo
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
	info := Info{Changed: "0", MainAhead: Dash, MainBehind: Dash, WIP: Dash, LocalOnly: Dash, Gone: Dash, RemoteOnly: Dash}

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

	// the default branch differs per repo (main / dev / ...), so read it from origin/HEAD
	if base, err := git(path, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && base != "" {
		info.Base = strings.TrimPrefix(base, "origin/")
	}

	list, err := Branches(path, info.Base, branch)
	if err != nil {
		return info
	}
	info.Branches = list
	var wip, local, gone, remote int
	for _, b := range list {
		switch b.Where {
		case WhereDefault:
			info.MainAhead, info.MainBehind = b.Push, b.Pull
		case WhereWIP:
			wip++
		case WhereLocalOnly:
			local++
			if b.Gone {
				gone++
			}
		case WhereRemoteOnly:
			remote++
		}
	}
	info.WIP, info.LocalOnly, info.Gone, info.RemoteOnly = strconv.Itoa(wip), strconv.Itoa(local), strconv.Itoa(gone), strconv.Itoa(remote)
	return info
}

// Where is where a branch exists.
type Where int

const (
	WhereDefault    Where = iota // the default branch (origin/HEAD), wherever it is
	WhereWIP                     // both locally and on origin
	WhereLocalOnly               // only locally
	WhereRemoteOnly              // only on origin
)

// String is the name the table and the JSON output use.
func (w Where) String() string {
	switch w {
	case WhereDefault:
		return "default"
	case WhereWIP:
		return "wip"
	case WhereLocalOnly:
		return "local-only"
	default:
		return "remote-only"
	}
}

// BranchInfo is one branch, local or on origin.
type BranchInfo struct {
	Name    string
	Where   Where
	Current bool
	// Gone: a local-only branch whose upstream was deleted on the remote
	Gone bool
	// Push / Pull compare the local branch with origin/<same name>: commits
	// not pushed / not pulled. Dash when either side is missing
	Push string
	Pull string
	// LastCommit is the committer date (YYYY-MM-DD) of the local branch, or
	// of the remote one when there is no local branch
	LastCommit string
}

// Branches lists the local and origin branches, matched by name, the default
// branch first and the rest by name. It never fetches; the tracking refs are
// read as they are.
func Branches(path, base, current string) ([]BranchInfo, error) {
	locals, err := git(path, "for-each-ref", "refs/heads", "--format=%(refname:short)\t%(upstream:track)\t%(committerdate:short)")
	if err != nil {
		return nil, err
	}
	remotes, err := git(path, "for-each-ref", "refs/remotes/origin", "--format=%(refname:lstrip=3)\t%(committerdate:short)")
	if err != nil {
		return nil, err
	}

	type local struct{ track, date string }
	byName := map[string]local{}
	for _, line := range lines(locals) {
		f := strings.SplitN(line, "\t", 3)
		if len(f) == 3 {
			byName[f[0]] = local{track: f[1], date: f[2]}
		}
	}
	onOrigin := map[string]string{}
	for _, line := range lines(remotes) {
		name, date, ok := strings.Cut(line, "\t")
		// origin/HEAD is a pointer to the default branch, not a branch
		if ok && name != "HEAD" {
			onOrigin[name] = date
		}
	}

	var out []BranchInfo
	add := func(name string) {
		l, isLocal := byName[name]
		rdate, isRemote := onOrigin[name]
		b := BranchInfo{Name: name, Current: name == current, Push: Dash, Pull: Dash, LastCommit: l.date}
		switch {
		case name == base:
			b.Where = WhereDefault
		case isLocal && isRemote:
			b.Where = WhereWIP
		case isLocal:
			b.Where = WhereLocalOnly
			b.Gone = strings.Contains(l.track, "gone")
		default:
			b.Where = WhereRemoteOnly
		}
		if !isLocal {
			b.LastCommit = rdate
		}
		if isLocal && isRemote {
			if counts, err := git(path, "rev-list", "--left-right", "--count", "refs/heads/"+name+"...refs/remotes/origin/"+name); err == nil {
				if f := strings.Fields(counts); len(f) >= 2 {
					b.Push, b.Pull = f[0], f[1]
				}
			}
		}
		out = append(out, b)
	}
	seen := map[string]bool{}
	for name := range byName {
		seen[name] = true
	}
	for name := range onOrigin {
		seen[name] = true
	}
	for name := range seen {
		add(name)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Where == WhereDefault) != (out[j].Where == WhereDefault) {
			return out[i].Where == WhereDefault
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
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
