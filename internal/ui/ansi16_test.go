package ui

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Colors are expressed only as ANSI-16 slot numbers, leaving the actual hues to
// the terminal theme. 256-color (38;5;N) / truecolor (38;2;R;G;B) / hex become
// fixed colors that do not follow the terminal theme, so they are banned across
// the whole repo. Where colors may be written is also limited to two places:
// theme.go (lipgloss) and render.go (raw SGR).
func TestANSI16Only(t *testing.T) {
	var (
		sgrExtended = regexp.MustCompile(`[34]8;[25];`)
		hexColor    = regexp.MustCompile(`"#[0-9a-fA-F]{3,8}"`)
		rawEscape   = regexp.MustCompile(`\\x1b|\\033|\\u001b`)
		colorCall   = regexp.MustCompile(`lipgloss\.Color\(`)
		digitString = regexp.MustCompile(`"([0-9]+)"`)
	)

	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		code := string(src)

		if sgrExtended.MatchString(code) {
			t.Errorf("%s: 256色/truecolor の SGR (38;5; / 38;2;) は禁止。ANSI-16 の slot を使う", rel)
		}
		if hexColor.MatchString(code) {
			t.Errorf("%s: hex 色の hardcode は禁止。ANSI-16 の slot を使う", rel)
		}
		if rawEscape.MatchString(code) && rel != filepath.Join("internal", "render", "render.go") {
			t.Errorf("%s: 生の escape sequence は render.go 以外に書かない", rel)
		}
		if colorCall.MatchString(code) && rel != filepath.Join("internal", "ui", "theme.go") {
			t.Errorf("%s: lipgloss.Color は theme.go 以外で呼ばない", rel)
		}
		if rel == filepath.Join("internal", "ui", "theme.go") {
			for _, m := range digitString.FindAllStringSubmatch(code, -1) {
				if n, err := strconv.Atoi(m[1]); err == nil && n > 15 {
					t.Errorf("theme.go: %q は ANSI-16 の範囲外 (0-15 のみ。16 以上は 256色扱いになる)", m[1])
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
