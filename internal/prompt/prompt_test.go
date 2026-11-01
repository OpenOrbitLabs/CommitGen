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

func TestValid(t *testing.T) {
	good := []string{"feat(auth): add login", "fix: handle nil", "refactor(ui)!: drop old theme\n\n- x"}
	bad := []string{"This appears to be a Git diff output", "Updated files", "feat add login", ""}
	for _, m := range good {
		if !Valid(m, "conventional") {
			t.Errorf("expected valid: %q", m)
		}
	}
	for _, m := range bad {
		if Valid(m, "conventional") {
			t.Errorf("expected invalid: %q", m)
		}
	}
	if Valid("This change updates things", "simple") {
		t.Error("prose should be invalid in simple style")
	}
}

func TestCleanExtractsFromProse(t *testing.T) {
	in := "Sure! Based on the diff, here is a message:\n\n**feat(ui): add dark mode toggle**\n- adds toggle\n```"
	want := "feat(ui): add dark mode toggle\n\n- adds toggle"
	if got := Clean(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestUserEndsWithTask(t *testing.T) {
	u := User(Input{Diff: "diff --git a/x b/x\n+1\n", Style: "conventional"})
	if !strings.Contains(u[strings.LastIndex(u, "Staged diff"):], "Task: write the git commit message") {
		t.Error("task instruction must come after the diff")
	}
}
