package prompt

import (
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	cases := map[string]string{
		"```\nfeat: add x\n```":                       "feat: add x",
		"```text\nfix(api): handle nil\n```":          "fix(api): handle nil",
		"Here's the commit message:\nfeat: add login": "feat: add login",
		"\"chore: bump deps\"":                        "chore: bump deps",
		"<think>hmm</think>\nfeat: add y":             "feat: add y",
		"feat: add z\n- one\n- two":                   "feat: add z\n\n- one\n- two",
		"feat: a  \r\n\r\n- b  ":                      "feat: a\n\n- b",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateKeepsAllFileHeaders(t *testing.T) {
	var b strings.Builder
	for _, f := range []string{"a.go", "b.go", "c.go", "d.go"} {
		b.WriteString("diff --git a/" + f + " b/" + f + "\n")
		b.WriteString(strings.Repeat("+line of code\n", 300))
	}
	out := Truncate(b.String(), 2000)
	for _, f := range []string{"a.go", "b.go", "c.go", "d.go"} {
		if !strings.Contains(out, "diff --git a/"+f) {
			t.Errorf("header for %s missing", f)
		}
	}
	if len(out) > 4000 {
		t.Errorf("truncated diff too long: %d", len(out))
	}
}

func TestSystemScopes(t *testing.T) {
	s := System(Input{Style: "conventional", Scopes: []string{"api", "ui"}})
	if !strings.Contains(s, "api, ui") {
		t.Error("scopes not in prompt")
	}
	if strings.Contains(System(Input{Style: "simple"}), "Conventional") {
		t.Error("simple style should not mention Conventional Commits")
	}
}
