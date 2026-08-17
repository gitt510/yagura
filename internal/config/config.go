// Package config reads the config file (TOML). The Config struct is the
// source of truth, split into one table per feature (repos / procs). To add
// a field, add it here with a toml tag and add an example to
// config.example.toml. A test guarantees the example decodes into the
// source of truth.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the source of truth. Table name = feature name.
type Config struct {
	Repos    Repos    `toml:"repos"`
	Sessions Sessions `toml:"sessions"`
}

// Repos configures the repos view (drift).
type Repos struct {
	Roots []string `toml:"roots"`
	// Interval is the auto-refresh interval. It fetches, so the default is
	// slower than procs
	Interval Duration `toml:"interval"`
	// TmuxSession is the tmux session that enter opens repos into. Empty
	// keeps enter inert; tmux is required only when this is used
	TmuxSession string `toml:"tmux-session"`
}

// Sessions configures the sessions view.
type Sessions struct {
	// Commands are the process names to watch, matched against the
	// basename of comm
	Commands []string `toml:"commands"`
	// Interval is the auto-refresh interval. Collection is cheap (no fetch),
	// so the default is fast
	Interval Duration `toml:"interval"`
}

// Defaults for anything left undeclared. Every behavioral default lives here.
var (
	defaultCommands         = []string{"claude"}
	defaultReposInterval    = Duration(time.Minute)
	defaultSessionsInterval = Duration(10 * time.Second)
)

// Duration reads a TOML string ("30s" / "1m") as a time.Duration.
type Duration time.Duration

// UnmarshalText is the toml decode hook. Zero or less would stop the
// watching, so it is rejected.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	if v <= 0 {
		return fmt.Errorf("must be positive: %s", b)
	}
	*d = Duration(v)
	return nil
}

// Example is the bundled config example. It is embedded so that the setup
// guidance and the document in the repo are one and the same.
//
//go:embed config.example.toml
var Example string

// Path is where the config file lives.
// os.UserConfigDir is avoided because on darwin it points at
// Library/Application Support, which does not match the ~/.config premise.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".config", "yagura", "config.toml")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "yagura", "config.toml")
}

// Load reads the config file. An unknown key is most likely a typo, so it is
// an error rather than silently dropped. A missing file is not a fault, so it
// returns a Config holding only the defaults.
func Load() (Config, error) {
	var c Config
	md, err := toml.DecodeFile(Path(), &c)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, err
	}
	if err == nil {
		if undec := md.Undecoded(); len(undec) > 0 {
			return Config{}, fmt.Errorf("%s: unknown key %q", Path(), undec[0].String())
		}
	}
	if len(c.Sessions.Commands) == 0 {
		c.Sessions.Commands = defaultCommands
	}
	if c.Repos.Interval == 0 {
		c.Repos.Interval = defaultReposInterval
	}
	if c.Sessions.Interval == 0 {
		c.Sessions.Interval = defaultSessionsInterval
	}
	return c, nil
}

// Skeleton is Example indented for embedding in the setup guidance.
func Skeleton() string {
	var b strings.Builder
	for line := range strings.Lines(Example) {
		b.WriteString("  " + line)
	}
	return b.String()
}
