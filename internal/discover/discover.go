// Package discover は宣言された root から対象 repo を洗い出す。
//
// 規則は 2 つだけ。root 自身が repo ならそれが対象、そうでなければ
// 1 段下の子だけを見る。再帰はしない。深く掘らないので、どこが対象に
// なるかを宣言だけから読み切れる。
package discover

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Repo は 1 行ぶんの repo 識別情報。
type Repo struct {
	Path  string // symlink を解いた絶対パス
	Name  string // 表示用のフルパス ($HOME は ~)
	Group string // 宣言された root ($HOME は ~)。表の器になる
	Base  string // repo 名
}

// Repos は roots を宣言順に辿り、見つけた repo と非致命的な警告を返す。
// query は repo の絶対パスに対する部分一致。空なら全件。
func Repos(roots []string, query string) ([]Repo, []string) {
	var (
		repos    []Repo
		warnings []string
		seen     = map[string]bool{}
	)

	for _, decl := range roots {
		label, resolved, err := resolveRoot(decl)
		if err != nil {
			// 宣言が 1 つ古いだけで表全体を殺さない
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
			// 宣言が重なっても 1 度だけ。先に宣言した root の並びが勝つ
			if seen[r.Path] || !strings.Contains(r.Path, query) {
				continue
			}
			seen[r.Path] = true
			repos = append(repos, r)
		}
	}

	return repos, warnings
}

// scan は 2 つの規則を適用する。
func scan(label, root string) ([]Repo, error) {
	if isRepo(root) {
		return []Repo{{
			Path:  root,
			Name:  label,
			Group: label,
			Base:  filepath.Base(label),
		}}, nil
	}

	// os.ReadDir は名前順に返すので、root 内の並びはそれに従う
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var repos []Repo
	for _, e := range entries {
		// symlink の子は辿らない。追いたい先は root として宣言してもらう。
		// ReadDir の DirEntry は symlink を dir と見ないので IsDir だけで落ちる
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

// isRepo は .git があるかだけを見る。worktree の .git は file なので種類は問わない。
// bare repo は .git を持たないので、ここで自然に外れる。
func isRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// resolveRoot は宣言を表示用の label と、走査用に symlink を解いた実体に分ける。
// label が宣言のままなのは、symlink の宣言を解決後の姿に化けさせないため。
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

// reason は PathError の重複したパス表示を落として読みやすくする。
func reason(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
