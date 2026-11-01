// Package prompt builds the LLM prompt and cleans up the model's answer.
package prompt

import (
	"fmt"
	"regexp"
	"strings"
)

type Input struct {
	Diff         string
	Stat         string
	Branch       string
	History      []string
	Style        string
	Language     string
	Scopes       []string
	Instructions string
	Hint         string // user-provided context via -m / --hint
	MaxDiff      int
}

func System(in Input) string {
	var b strings.Builder
	b.WriteString("You are an expert software engineer who writes precise git commit messages.\n\nRules:\n")
	if in.Style == "simple" {
		b.WriteString("- First line: a concise summary in imperative mood (\"Add\", \"Fix\", not \"Added\"), max 72 characters, no trailing period.\n")
	} else {
		b.WriteString("- Use Conventional Commits. First line: <type>(<optional scope>): <summary>\n")
		b.WriteString("- Allowed types: feat, fix, refactor, perf, docs, test, build, ci, chore, style, revert.\n")
		b.WriteString("- Summary in imperative mood, lowercase start, max 72 characters total, no trailing period.\n")
		if len(in.Scopes) > 0 {
			fmt.Fprintf(&b, "- Scope, if used, must be one of: %s.\n", strings.Join(in.Scopes, ", "))
		}
	}
	b.WriteString("- If the change is non-trivial, add a body after one blank line: short bullet points (\"- \") explaining WHAT changed and WHY. Wrap at 72 characters.\n")
	b.WriteString("- Do not describe every file. Focus on intent. Never invent changes that are not in the diff.\n")
	b.WriteString("- Output ONLY the commit message text. No code fences, no quotes, no preamble.\n")
	if in.Language != "" && !strings.EqualFold(in.Language, "english") {
		fmt.Fprintf(&b, "- Write the message in %s (keep the type keyword in English).\n", in.Language)
	}
	if in.Instructions != "" {
		fmt.Fprintf(&b, "- %s\n", in.Instructions)
	}
	return b.String()
}

func User(in Input) string {
	var b strings.Builder
	if in.Hint != "" {
		fmt.Fprintf(&b, "Context from the author: %s\n\n", in.Hint)
	}
	if in.Branch != "" {
		fmt.Fprintf(&b, "Branch: %s\n\n", in.Branch)
	}
	if len(in.History) > 0 {
		b.WriteString("Recent commit subjects in this repo (match their style):\n")
		for _, h := range in.History {
			fmt.Fprintf(&b, "  %s\n", h)
		}
		b.WriteString("\n")
	}
	if in.Stat != "" {
		fmt.Fprintf(&b, "Files changed:\n%s\n", in.Stat)
	}
	diff := in.Diff
	if in.MaxDiff > 0 && len(diff) > in.MaxDiff {
		diff = Truncate(diff, in.MaxDiff)
	}
	fmt.Fprintf(&b, "Staged diff:\n%s\n", diff)
	// Repeat the task after the diff: small local models weight the end of the
	// prompt most, and otherwise tend to *describe* the diff instead.
	b.WriteString("\n---\nTask: write the git commit message for the staged changes above.\n")
	if in.Style == "simple" {
		b.WriteString("Reply with ONLY the commit message: a short imperative summary line, optionally followed by a blank line and bullet points.\n")
	} else {
		b.WriteString("Reply with ONLY the commit message. The first line MUST look like `type(scope): summary`, for example `feat(auth): add password reset`. Do not describe or explain the diff.\n")
	}
	return b.String()
}

// RepairUser asks the model to turn a bad answer (e.g. a prose description of
// the diff) into a proper commit message.
func RepairUser(bad, stat string, style string) string {
	format := "`type(scope): summary` (Conventional Commits)"
	if style == "simple" {
		format = "a short imperative summary line"
	}
	return "The following text is NOT a valid git commit message:\n\n" + bad +
		"\n\nFiles changed:\n" + stat +
		"\nRewrite it as a git commit message. First line: " + format +
		", max 72 characters. Then optionally a blank line and up to 5 short bullet points. Reply with ONLY the commit message."
}

var conventionalRe = regexp.MustCompile(`^(feat|fix|refactor|perf|docs|test|build|ci|chore|style|revert)(\([^)\n]+\))?!?: \S`)

// Valid reports whether msg looks like a usable commit message for the style.
func Valid(msg, style string) bool {
	subj, _, _ := strings.Cut(strings.TrimSpace(msg), "\n")
	if subj == "" || len(subj) > 100 {
		return false
	}
	if style == "simple" {
		low := strings.ToLower(subj)
		return !strings.HasPrefix(low, "this ") && !strings.HasPrefix(low, "the ") && !strings.HasPrefix(low, "here")
	}
	return conventionalRe.MatchString(subj)
}

// extractConventional finds a conventional subject line buried inside prose
// and returns the message starting from it.
func extractConventional(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		t := strings.Trim(strings.TrimSpace(l), "`*\"")
		if conventionalRe.MatchString(t) {
			lines[i] = t
			return strings.Join(lines[i:], "\n")
		}
	}
	return s
}

// Truncate shortens a diff while keeping every file header, so the model still
// sees which files changed even when their hunks are cut.
func Truncate(diff string, max int) string {
	files := strings.Split(diff, "\ndiff --git ")
	if len(files) == 0 {
		return diff
	}
	for i := 1; i < len(files); i++ {
		files[i] = "diff --git " + files[i]
	}
	per := max / len(files)
	if per < 400 {
		per = 400
	}
	var b strings.Builder
	for _, f := range files {
		if b.Len() >= max {
			// Keep just the header line for the remaining files.
			line, _, _ := strings.Cut(f, "\n")
			b.WriteString(line + "\n[... hunks omitted ...]\n")
			continue
		}
		if len(f) > per {
			cut := strings.LastIndex(f[:per], "\n")
			if cut < 0 {
				cut = per
			}
			b.WriteString(f[:cut] + "\n[... truncated ...]\n")
		} else {
			b.WriteString(f)
			if !strings.HasSuffix(f, "\n") {
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

var (
	fenceRe  = regexp.MustCompile("(?s)^```[a-zA-Z]*\\n(.*?)\\n?```$")
	thinkRe  = regexp.MustCompile(`(?s)<think>.*?</think>`)
	prefixRe = regexp.MustCompile(`(?i)^(here('s| is) (the|a|your) commit message:?|commit message:)\s*`)
)

// Clean strips reasoning tags, code fences, preambles and surrounding quotes.
func Clean(s string) string {
	s = thinkRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = prefixRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if m := fenceRe.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		s = s[1 : len(s)-1]
	}
	// Normalise line endings and trailing whitespace.
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	s = strings.TrimSpace(strings.Join(lines, "\n"))
	// If the model wrapped the message in prose, pull out the real message.
	if subj, _, _ := strings.Cut(s, "\n"); !conventionalRe.MatchString(subj) {
		s = extractConventional(s)
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	// Ensure a blank line between subject and body.
	if subj, body, ok := strings.Cut(s, "\n"); ok && !strings.HasPrefix(body, "\n") {
		s = subj + "\n\n" + strings.TrimLeft(body, "\n")
	}
	return s
}
