package render

import (
	"encoding/json"
	"io"
)

// The JSON records are the machine-readable counterpart of the tables: the
// same collected facts, but as values instead of display strings. A count
// that means nothing — no upstream to compare against, or a fetch that
// failed — is null, never 0.

// RepoRecord is one repo's drift.
type RepoRecord struct {
	// Root is the declared root this repo was found under, as the table's
	// group header shows it (with $HOME as ~). Path is the actionable one.
	Root string `json:"root"`
	Name string `json:"name"`
	Path string `json:"path"`

	Head      string `json:"head"`
	HeadState string `json:"head_state"`

	Changed *int `json:"changed"`
	// MainAhead / MainBehind: the local default branch against origin
	MainAhead  *int `json:"main_ahead"`
	MainBehind *int `json:"main_behind"`
	// Every other branch is counted in exactly one of WIP / LocalOnly /
	// RemoteOnly; Gone is the part of LocalOnly deleted on the remote
	WIP        *int `json:"wip"`
	LocalOnly  *int `json:"local_only"`
	Gone       *int `json:"gone"`
	RemoteOnly *int `json:"remote_only"`

	Branches []BranchRecord `json:"branches"`

	// FetchFailed tells the null counts above apart from a repo that simply
	// has no upstream: here the remote is unknown, not absent.
	FetchFailed bool `json:"fetch_failed"`
}

// BranchRecord is one branch of a repo.
type BranchRecord struct {
	Name    string `json:"name"`
	Where   string `json:"where"` // default / wip / local-only / remote-only
	Current bool   `json:"current"`
	Gone    bool   `json:"gone"`
	// Push / Pull: the local branch against origin/<same name>; null when
	// either side is missing or the fetch failed
	Push       *int    `json:"push"`
	Pull       *int    `json:"pull"`
	LastCommit *string `json:"last_commit"`
}

// SessionRecord is one running agent CLI session.
type SessionRecord struct {
	Command string `json:"command"`
	PID     int    `json:"pid"`
	CWD     string `json:"cwd"`

	// Branch / Changed are null when cwd is not inside a work tree.
	Branch    *string `json:"branch"`
	HeadState string  `json:"head_state"`
	Changed   *int    `json:"changed"`

	// Tmux is session:window.pane, null outside a pane.
	Tmux    *string  `json:"tmux"`
	Elapsed string   `json:"elapsed"`
	CPUPct  *float64 `json:"cpu_pct"`
	RSSKB   int      `json:"rss_kb"`
}

type reposDoc struct {
	Repos    []RepoRecord `json:"repos"`
	Warnings []string     `json:"warnings"`
}

type sessionsDoc struct {
	Sessions []SessionRecord `json:"sessions"`
	Warnings []string        `json:"warnings"`
}

// ReposJSON writes the drift document.
func ReposJSON(w io.Writer, repos []RepoRecord, warnings []string) error {
	return encode(w, reposDoc{Repos: nonNil(repos), Warnings: nonNil(warnings)})
}

// SessionsJSON writes the sessions document.
func SessionsJSON(w io.Writer, sessions []SessionRecord, warnings []string) error {
	return encode(w, sessionsDoc{Sessions: nonNil(sessions), Warnings: nonNil(warnings)})
}

func encode(w io.Writer, doc any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

// nonNil keeps an empty list an empty list: a consumer counting or iterating
// should never have to special-case null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
