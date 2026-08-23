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

	Changed  *int `json:"changed"`
	Ahead    *int `json:"ahead"`
	Behind   *int `json:"behind"`
	Unmerged *int `json:"unmerged"`

	// FetchFailed tells the null counts above apart from a repo that simply
	// has no upstream: here the remote is unknown, not absent.
	FetchFailed bool `json:"fetch_failed"`
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
