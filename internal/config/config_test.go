package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "yagura"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "yagura", "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The config contract: read from one table per feature, preserving the order
// of declaration. An unknown key is an error; a missing file is fine and
// yields the defaults.
func TestLoad(t *testing.T) {
	writeConfig(t, `
[repos]
roots = [
  "~/ghq/github.com/gitt510", # 直下の子を見る
  "~/dotfiles",
]
tmux-session = "work"
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos.Roots) != 2 || cfg.Repos.Roots[0] != "~/ghq/github.com/gitt510" || cfg.Repos.Roots[1] != "~/dotfiles" {
		t.Errorf("roots = %v", cfg.Repos.Roots)
	}
	if cfg.Repos.TmuxSession != "work" {
		t.Errorf("tmux-session = %q, want work", cfg.Repos.TmuxSession)
	}
}

// The interval contract: read in "30s" form; a broken value or zero or less
// is an error; undeclared falls back to the default (1m).
func TestLoadIntervals(t *testing.T) {
	writeConfig(t, "[repos]\ninterval = \"30s\"\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfg.Repos.Interval) != 30*time.Second {
		t.Errorf("interval = %v, want 30s", cfg.Repos.Interval)
	}

	writeConfig(t, "[repos]\ninterval = \"fast\"\n")
	if _, err := Load(); err == nil {
		t.Error("壊れた duration が通った")
	}

	writeConfig(t, "[repos]\ninterval = \"0s\"\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "positive") {
		t.Errorf("err = %v, want positive", err)
	}

	writeConfig(t, "[repos]\nroots = [\"~/dotfiles\"]\n")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if time.Duration(cfg.Repos.Interval) != time.Minute {
		t.Errorf("既定の interval = %v, want 1m", cfg.Repos.Interval)
	}
}

func TestLoadUnknownKey(t *testing.T) {
	writeConfig(t, "[repos]\ndepth = 2\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("err = %v, want unknown key", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repos.Roots != nil {
		t.Errorf("roots = %v, want nil", cfg.Repos.Roots)
	}
}

// Guarantees the example decodes into the source of truth (the Config struct).
func TestExampleDecodes(t *testing.T) {
	writeConfig(t, Example)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("config.example.toml が読めない: %v", err)
	}
	if len(cfg.Repos.Roots) == 0 {
		t.Error("config.example.toml に roots の例がない")
	}
}
