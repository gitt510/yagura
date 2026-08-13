// Package config は設定ファイル (TOML) を読む。Config struct が config の
// 原本で、feature ごとの table (repos / procs) に分かれる。field を足すときは
// ここに toml tag 付きで足し、config.example.toml に例を足す。example が
// 原本どおりに読めることは test が保証する。
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

// Config が config の原本。table 名 = feature 名。
type Config struct {
	Repos    Repos    `toml:"repos"`
	Sessions Sessions `toml:"sessions"`
}

// Repos は repos view (drift) の設定。
type Repos struct {
	Roots []string `toml:"roots"`
	// Interval は自動更新の間隔。fetch を伴うので procs より遅い既定
	Interval Duration `toml:"interval"`
}

// Sessions は sessions view の設定。
type Sessions struct {
	// Commands は監視する process 名。comm の basename に一致させる
	Commands []string `toml:"commands"`
	// Interval は自動更新の間隔。収集が軽い (fetch なし) ので速い既定
	Interval Duration `toml:"interval"`
}

// 未宣言時の既定値。挙動の既定はすべてここに集める。
var (
	defaultCommands         = []string{"claude"}
	defaultReposInterval    = Duration(time.Minute)
	defaultSessionsInterval = Duration(10 * time.Second)
)

// Duration は TOML の文字列 ("30s" / "1m") を time.Duration として読む。
type Duration time.Duration

// UnmarshalText は toml の decode 点。0 以下は監視が止まるので拒否する。
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

// Example は同梱の設定例。setup 案内と repo 内の文書を同一物にするため
// embed で持つ。
//
//go:embed config.example.toml
var Example string

// Path は設定ファイルの場所。
// os.UserConfigDir を使わないのは、darwin だと Library/Application Support を
// 指してしまい、~/.config に置く前提と合わないため。
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

// Load は設定ファイルを読む。知らない key は typo の可能性が高いので
// 黙って捨てず error にする。ファイルが無いのは異常ではないので
// 既定値だけの Config を返す。
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

// Skeleton は setup 案内に埋め込む用に Example を indent したもの。
func Skeleton() string {
	var b strings.Builder
	for line := range strings.Lines(Example) {
		b.WriteString("  " + line)
	}
	return b.String()
}
