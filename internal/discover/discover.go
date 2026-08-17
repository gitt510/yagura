// Package discover finds the target repos under the declared roots.
//
// There are only two rules. If a root is itself a repo, that root is the
// target; otherwise only its immediate children are considered. No
// recursion. Because it never digs deep, the declarations alone tell you
// exactly which repos are in scope.
package discover

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Repo identifies a single repo, one table row's worth.
type Repo struct {
	Path  string // absolute path with symlinks resolved
	Name  string // full path for display ($HOME shown as ~)
	Group string // declared root ($HOME shown as ~); the table's container
	Base  string // repo name
}

// Repos walks roots in declaration order and returns the repos it found
// along with non-fatal warnings. query is a substring match against the
// repo's absolute path; empty matches everything.
func Repos(roots []string, query string) ([]Repo, []string) {
	var (
		repos    []Repo
		warnings []string
		seen     = map[string]bool{}
	)

	for _, decl := range roots {
		label, resolved, err := resolveRoot(decl)
		if err != nil {
			// one stale declaration must not kill the whole table
			warnings = append(warnings, fmt.Sprintf("cannot resolve root: %s: %s", label, reason(err)))
			continue
		}

		found, err := scan(label, resolved)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cannot read root: %s: %s", label, reason(err)))
			continue
		}
		if len(found) == 0 {
			warnings = append(warnings, fmt.Sprintf("no repos under root: %s", label))
			continue
		}

		for _, r := range found {
			// list a repo once even if roots overlap; the earlier root wins
			if seen[r.Path] || !strings.Contains(r.Path, query) {
				continue
			}
			seen[r.Path] = true
			repos = append(repos, r)
		}
	}

	return repos, warnings
}

// scan applies the two rules.
func scan(label, root string) ([]Repo, error) {
	if isRepo(root) {
		return []Repo{{
			Path:  root,
			Name:  label,
			Group: label,
			Base:  filepath.Base(label),
		}}, nil
	}

	// os.ReadDir returns entries sorted by name, so ordering within a root
	// follows that
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var repos []Repo
	for _, e := range entries {
		// don't follow symlinked children; declare the target as a root
		// instead. ReadDir's DirEntry doesn't report a symlink as a dir,
		// so IsDir alone drops them
		if !e.IsDir() {
			continue
		}
		child := filepath.Join(root, e.Name())
		if !isRepo(child) {
			continue
		}
		repos = append(repos, Repo{
			Path:  child,
			Name:  label + "/" + e.Name(),
			Group: label,
			Base:  e.Name(),
		})
	}
	return repos, nil
}

// isRepo only checks whether .git exists. A worktree's .git is a file, so
// the kind doesn't matter. A bare repo has no .git, so it drops out here.
func isRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// resolveRoot splits a declaration into a label for display and the
// symlink-resolved path used for scanning. The label stays as declared so a
// symlinked declaration doesn't morph into its resolved form.
func resolveRoot(decl string) (label, resolved string, err error) {
	abs, err := filepath.Abs(expand(strings.TrimSpace(decl)))
	if err != nil {
		return decl, "", err
	}
	label = abbreviate(abs)

	resolved, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return label, "", err
	}
	return label, resolved, nil
}

func expand(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return path
}

func abbreviate(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}

// reason strips PathError's redundant path echo to keep the message readable.
func reason(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
