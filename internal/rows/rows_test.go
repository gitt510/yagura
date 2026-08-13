package rows

import (
	"testing"

	"github.com/gitt510/yagura/internal/discover"
	"github.com/gitt510/yagura/internal/gitinfo"
	"github.com/gitt510/yagura/internal/procs"
	"github.com/gitt510/yagura/internal/render"
)

// fetch 失敗時の約束: remote 由来の列だけ x になり、local の事実と
// 構造的な - はそのまま残る。
func TestOneFetchFailed(t *testing.T) {
	repo := discover.Repo{Group: "~/g", Base: "r"}
	info := gitinfo.Info{
		Changed: "3", Head: "main", Branch: "main", Base: "main",
		Ahead: "1", Behind: "2", Unmerged: gitinfo.Dash,
	}

	got := One(repo, info)
	if got.Ahead != "1" || got.Behind != "2" || got.Unmerged != gitinfo.Dash {
		t.Errorf("fetch 成功時に値が変わった: %+v", got)
	}

	info.FetchFailed = true
	got = One(repo, info)
	if got.Ahead != Unsynced || got.Behind != Unsynced {
		t.Errorf("AHEAD/BEHIND = %q, %q, want %q", got.Ahead, got.Behind, Unsynced)
	}
	if got.Unmerged != gitinfo.Dash {
		t.Errorf("UNMERGED = %q, want %q (構造的な dash は残す)", got.Unmerged, gitinfo.Dash)
	}
	if got.Changed != "3" || got.Head != "main" {
		t.Errorf("local の事実が変わった: CHANGED %q, HEAD %q", got.Changed, got.Head)
	}
}

// Sessions の約束: home は ~ に縮み、取れなかった値は - になり、cwd 順に並ぶ。
func TestSessions(t *testing.T) {
	home := "/Users/t"
	got := Sessions([]procs.Proc{
		{PID: 300, CWD: "/Users/t/z-repo", Tmux: "work:1.2"},
		{PID: 400, CWD: "/Users/t/a-repo"},
		{PID: 500, Tmux: "work:2.1"},
		{PID: 600, CWD: "/Users/t"},
	}, home, nil)

	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	// - < ~ < ~/a-repo < ~/z-repo
	if got[0].CWD != "-" || got[0].PID != "500" {
		t.Errorf("got[0] = %+v, want cwd - の行が先頭", got[0])
	}
	if got[1].CWD != "~" {
		t.Errorf("got[1].CWD = %q, want ~ (home そのもの)", got[1].CWD)
	}
	if got[2].CWD != "~/a-repo" || got[3].CWD != "~/z-repo" {
		t.Errorf("cwd 順に並んでいない: %q, %q", got[2].CWD, got[3].CWD)
	}
	if got[3].Tmux != "work:1.2" || got[2].Tmux != "-" {
		t.Errorf("TMUX = %q, %q", got[3].Tmux, got[2].Tmux)
	}
}

// git 合流の約束: cwd が git に載っていれば BRANCH / CHG が入り、
// 載っていなければ - のまま。key は縮める前の実 path。
func TestProcsGit(t *testing.T) {
	git := map[string]gitinfo.Info{
		"/Users/t/repo": {Changed: "2", Head: "feature", Branch: "feature", Base: "main"},
	}
	// cwd sort で ~/not-repo が先、~/repo が後に来る
	got := Sessions([]procs.Proc{
		{PID: 300, CWD: "/Users/t/repo"},
		{PID: 400, CWD: "/Users/t/not-repo"},
	}, "/Users/t", git)

	if got[1].Branch != "feature" || got[1].Changed != "2" {
		t.Errorf("repo の行 = %+v, want BRANCH feature / CHG 2", got[1])
	}
	if got[1].HeadState != render.HeadBranch {
		t.Errorf("HeadState = %v, want HeadBranch", got[1].HeadState)
	}
	if got[0].Branch != gitinfo.Dash || got[0].Changed != gitinfo.Dash {
		t.Errorf("repo 外の行 = %+v, want - のまま", got[0])
	}
}

// CWDs の約束: 空の cwd は落とす。git を引く対象の一覧になる。
func TestCWDs(t *testing.T) {
	got := CWDs([]procs.Proc{{CWD: "/a"}, {CWD: ""}, {CWD: "/a"}})
	if len(got) != 2 || got[0] != "/a" || got[1] != "/a" {
		t.Errorf("CWDs = %v, want [/a /a]", got)
	}
}

// elapsedLabel の約束: etime の 4 形式を上位 2 単位に縮め、読めなければ -。
func TestElapsedLabel(t *testing.T) {
	cases := map[string]string{
		"00:42":       "42s",
		"05:42":       "5m",
		"01:02:03":    "1h2m",
		"10-01:02:03": "10d1h",
		"":            "-",
		"garbage":     "-",
	}
	for in, want := range cases {
		if got := elapsedLabel(in); got != want {
			t.Errorf("elapsedLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCPUAndMemLabel(t *testing.T) {
	if got := cpuLabel("12.5"); got != "13%" {
		t.Errorf("cpuLabel(12.5) = %q, want 13%%", got)
	}
	if got := cpuLabel("0.0"); got != "0%" {
		t.Errorf("cpuLabel(0.0) = %q, want 0%%", got)
	}
	if got := cpuLabel("bad"); got != "-" {
		t.Errorf("cpuLabel(bad) = %q, want -", got)
	}

	memCases := map[int]string{0: "-", 512: "512K", 40960: "40M", 2097152: "2.0G"}
	for in, want := range memCases {
		if got := memLabel(in); got != want {
			t.Errorf("memLabel(%d) = %q, want %q", in, got, want)
		}
	}
}

// fishPath の約束: 最後の要素だけ残して頭文字に縮める。
// 隠し directory は 2 文字、~ と根の / と最終要素は縮まない。
func TestFishPath(t *testing.T) {
	cases := map[string]string{
		"~/ghq/github.com/gitt510/yagura": "~/g/g/g/yagura",
		"~/.config/nvim":                  "~/.c/nvim",
		"/opt/homebrew/bin":               "/o/h/bin",
		"~/dotfiles":                      "~/dotfiles",
		"~":                               "~",
		"-":                               "-",
	}
	for in, want := range cases {
		if got := fishPath(in); got != want {
			t.Errorf("fishPath(%q) = %q, want %q", in, got, want)
		}
	}
}
