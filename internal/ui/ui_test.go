package ui

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// 自動更新の間隔は view ごとに独立。repos を procs の速さで回すと
// git fetch が全 repo に高頻度で走ってしまう。
func TestIntervalPerView(t *testing.T) {
	m := newModel(Options{SessionsInterval: 10 * time.Second, ReposInterval: time.Minute})
	if got := m.interval(); got != time.Minute {
		t.Errorf("repos の interval = %s, want 1m", got)
	}
	m.view = viewSessions
	if got := m.interval(); got != 10*time.Second {
		t.Errorf("sessions の interval = %s, want 10s", got)
	}
}

// 0 単位を落とす処理は "10s" を "1" にしかねないので、境界を固定しておく。
func TestFormatInterval(t *testing.T) {
	cases := map[string]string{
		"1m":      "1m",
		"1h":      "1h",
		"3600s":   "1h",
		"1m30s":   "1m30s",
		"1h30m":   "1h30m",
		"90m":     "1h30m",
		"1h0m30s": "1h0m30s",
		"10s":     "10s",
		"100s":    "1m40s",
		"20m":     "20m",
		"5s":      "5s",
	}
	for in, want := range cases {
		d, err := time.ParseDuration(in)
		if err != nil {
			t.Fatalf("ParseDuration(%q): %v", in, err)
		}
		if got := formatInterval(d); got != want {
			t.Errorf("formatInterval(%s) = %q, want %q", d, got, want)
		}
	}
}

// spliceLine の約束: x から幅 w だけ box に置き換わり、短い base は
// 隙間を space で埋める。float が枠の途中の行でも列がずれないための基礎。
func TestSpliceLine(t *testing.T) {
	if got := spliceLine("abcdefghij", "XXX", 3, 3); got != "abcXXXghij" {
		t.Errorf("spliceLine = %q, want abcXXXghij", got)
	}
	if got := spliceLine("ab", "XXX", 4, 3); got != "ab  XXX" {
		t.Errorf("短い base = %q, want ab  XXX", got)
	}
	if got := spliceLine("", "XXX", 0, 3); got != "XXX" {
		t.Errorf("空 base = %q, want XXX", got)
	}
}

// gradientLine の約束: 塗り分けても文字そのものは欠けない。
func TestGradientLine(t *testing.T) {
	plain := []lipgloss.Style{{}, {}, {}}
	if got := gradientLine("abcdefg", plain); got != "abcdefg" {
		t.Errorf("gradientLine = %q, want abcdefg", got)
	}
}
