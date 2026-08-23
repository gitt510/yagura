package render

import (
	"strings"
	"testing"
)

// An empty result is still a list: a reader counting or iterating should
// never meet null where an array was promised.
func TestJSONEmptyListsAreArrays(t *testing.T) {
	var buf strings.Builder
	if err := ReposJSON(&buf, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"repos": []`, `"warnings": []`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %s in:\n%s", want, buf.String())
		}
	}

	buf.Reset()
	if err := SessionsJSON(&buf, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"sessions": []`) {
		t.Errorf("missing empty sessions in:\n%s", buf.String())
	}
}

// An unset count is null, and a zero count is 0 — the two must not collapse.
func TestJSONNullVsZero(t *testing.T) {
	zero := 0
	var buf strings.Builder
	if err := ReposJSON(&buf, []RepoRecord{{Name: "a", Changed: &zero}}, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"changed": 0`) {
		t.Errorf("zero count lost:\n%s", out)
	}
	if !strings.Contains(out, `"behind": null`) {
		t.Errorf("unset count is not null:\n%s", out)
	}
}

func TestHeadStateString(t *testing.T) {
	cases := map[HeadState]string{
		HeadUnknown:  "unknown",
		HeadDefault:  "default",
		HeadBranch:   "branch",
		HeadDetached: "detached",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("HeadState(%d).String() = %q, want %q", in, got, want)
		}
	}
}
