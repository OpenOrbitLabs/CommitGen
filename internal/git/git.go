// Package git wraps the few git commands commitgen needs.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Noise files that rarely help the model and eat the context budget.
var DefaultExcludes = []string{
	"*.lock", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "go.sum",
	"Cargo.lock", "poetry.lock", "composer.lock", "*.min.js", "*.min.css",
	"*.map", "dist/*", "build/*", "vendor/*", "*.svg", "*.png", "*.jpg",
}

var ErrNothingStaged = errors.New("nothing staged — run `git add` first")

func run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out.String(), nil
}

// RepoRoot returns the top-level directory of the current repository.
func RepoRoot() (string, error) {
	s, err := run("rev-parse", "--show-toplevel")
	return strings.TrimSpace(s), err
}

// GitDir returns the path of the .git directory (handles worktrees).
func GitDir() (string, error) {
	s, err := run("rev-parse", "--absolute-git-dir")
	return strings.TrimSpace(s), err
}

func excludeSpecs(extra []string) []string {
	specs := []string{"--", "."}
	for _, p := range append(append([]string{}, DefaultExcludes...), extra...) {
		specs = append(specs, ":(exclude)"+p)
	}
	return specs
}

// StagedDiff returns the staged diff with noisy files excluded.
func StagedDiff(extraExcludes []string) (string, error) {
	args := append([]string{"diff", "--cached", "--no-color", "--no-ext-diff", "-U3"}, excludeSpecs(extraExcludes)...)
	d, err := run(args...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(d) == "" {
		// Maybe only excluded files are staged — check the unfiltered stat.
		stat, _ := run("diff", "--cached", "--name-only")
		if strings.TrimSpace(stat) == "" {
			return "", ErrNothingStaged
		}
	}
	return d, nil
}

// StagedStat returns `git diff --cached --stat` (includes excluded files, so
// the model still knows they changed).
func StagedStat() (string, error) {
	return run("diff", "--cached", "--no-color", "--stat=120")
}

// RecentSubjects returns the last n commit subjects (empty in a fresh repo).
func RecentSubjects(n int) []string {
	if n <= 0 {
		return nil
	}
	s, err := run("log", fmt.Sprintf("-%d", n), "--no-merges", "--pretty=%s")
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Branch returns the current branch name (empty if detached).
func Branch() string {
	s, err := run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	s = strings.TrimSpace(s)
	if s == "HEAD" {
		return ""
	}
	return s
}
