// Package policy holds repo-wide convention tests that no single package
// can host: they assert rules about the whole source tree.
package policy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"
)

// TestNoJapaneseComments enforces English-only comments. gosmopolitan guards
// string literals but never looks at comments, so this test covers the gap.
func TestNoJapaneseComments(t *testing.T) {
	root := moduleRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		checkFile(t, root, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkFile(t *testing.T, root, path string) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if ast.IsGenerated(file) {
		return
	}
	for _, group := range file.Comments {
		for _, c := range group.List {
			if isDirective(c.Text) {
				continue
			}
			if pos, ok := firstJapanese(c.Text); ok {
				p := fset.Position(c.Pos() + token.Pos(pos))
				rel, err := filepath.Rel(root, p.Filename)
				if err != nil {
					rel = p.Filename
				}
				t.Errorf("%s:%d:%d: comment contains Japanese script", rel, p.Line, p.Column)
			}
		}
	}
}

// moduleRoot resolves the repository root from this file's location, since
// go test runs with the package directory as the working directory.
func moduleRoot(t *testing.T) string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// isDirective reports whether the comment is a tool directive such as
// //go:build or //go:generate rather than prose.
func isDirective(text string) bool {
	if !strings.HasPrefix(text, "//") {
		return false
	}
	rest := text[2:]
	if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
		return false
	}
	colon := strings.IndexByte(rest, ':')
	return colon > 0 && !strings.ContainsAny(rest[:colon], " \t")
}

// firstJapanese returns the byte offset of the first Han, Hiragana, or
// Katakana rune in s.
func firstJapanese(s string) (int, bool) {
	for i, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) {
			return i, true
		}
	}
	return 0, false
}
