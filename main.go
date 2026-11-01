// commitgen — generate git commit messages from your staged diff with a
// local or API-based LLM.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/onetwothird/commitgen/internal/config"
	"github.com/onetwothird/commitgen/internal/git"
	"github.com/onetwothird/commitgen/internal/llm"
	"github.com/onetwothird/commitgen/internal/prompt"
	"github.com/onetwothird/commitgen/internal/redact"
)

var version = "dev" // set with -ldflags "-X main.version=v1.0.0"

const usage = `commitgen — write commit messages from your staged diff

Usage:
  commitgen [flags]            generate a message for the staged changes
  commitgen hook install       draft messages automatically on ` + "`git commit`" + `
  commitgen hook uninstall
  commitgen config init        write a starter config file
  commitgen config path|show
  commitgen version

Flags:
`

type flags struct {
	commit, yes, print, dryRun, noRedact, verbose bool
	n                                             int
	hint, model, url, provider, style, lang, hook string
	maxDiff                                       int
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "hook":
			exit(hookCmd(args[1:]))
		case "config":
			exit(configCmd(args[1:]))
		case "version":
			fmt.Println("commitgen", version)
			return
		case "help":
			args = []string{"-h"}
		default:
			exit(fmt.Errorf("unknown command %q (see commitgen -h)", args[0]))
		}
	}

	var f flags
	fs := flag.NewFlagSet("commitgen", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage); fs.PrintDefaults() }
	fs.BoolVar(&f.commit, "c", false, "commit with the generated message (opens your editor to review)")
	fs.BoolVar(&f.yes, "y", false, "with -c: commit immediately, skip the editor")
	fs.BoolVar(&f.print, "p", false, "print the message only (no interactive menu); good for scripts")
	fs.BoolVar(&f.dryRun, "dry-run", false, "show exactly what would be sent to the model, then exit")
	fs.BoolVar(&f.noRedact, "no-redact", false, "do not mask secrets in the diff")
	fs.BoolVar(&f.verbose, "v", false, "verbose output")
	fs.IntVar(&f.n, "n", 1, "number of candidate messages to generate and pick from")
	fs.StringVar(&f.hint, "m", "", "extra context for the model, e.g. -m \"fixes login bug #42\"")
	fs.StringVar(&f.model, "model", "", "model name (overrides config)")
	fs.StringVar(&f.url, "url", "", "OpenAI-compatible base URL (overrides config)")
	fs.StringVar(&f.provider, "provider", "", "openai | anthropic (overrides config)")
	fs.StringVar(&f.style, "style", "", "conventional | simple (overrides config)")
	fs.StringVar(&f.lang, "lang", "", "language of the message (overrides config)")
	fs.IntVar(&f.maxDiff, "max", 0, "max diff characters sent to the model (overrides config)")
	fs.StringVar(&f.hook, "hook", "", "internal: write message into this file (used by the git hook)")
	fs.Parse(args)

	exit(generateCmd(f))
}

func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "commitgen:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func loadConfig(f flags) (config.Config, string, error) {
	root, err := git.RepoRoot()
	if err != nil {
		return config.Config{}, "", errors.New("not inside a git repository")
	}
	c, err := config.Load(root)
	if err != nil {
		return c, root, err
	}
	if f.model != "" {
		c.Model = f.model
	}
	if f.url != "" {
		c.BaseURL = f.url
	}
	if f.provider != "" {
		c.Provider = f.provider
	}
	if f.style != "" {
		c.Style = f.style
	}
	if f.lang != "" {
		c.Language = f.lang
	}
	if f.maxDiff > 0 {
		c.MaxDiff = f.maxDiff
	}
	return c, root, nil
}

func generateCmd(f flags) error {
	cfg, _, err := loadConfig(f)
	if err != nil {
		return err
	}

	diff, err := git.StagedDiff(cfg.Exclude)
	if err != nil {
		if f.hook != "" && errors.Is(err, git.ErrNothingStaged) {
			return nil // let git handle the empty commit itself
		}
		return err
	}
	stat, _ := git.StagedStat()
	if !f.noRedact {
		var n int
		diff, n = redact.Secrets(diff)
		if n > 0 {
			fmt.Fprintf(os.Stderr, "commitgen: masked %d possible secret(s) before sending\n", n)
		}
	}

	in := prompt.Input{
		Diff: diff, Stat: stat, Branch: git.Branch(), History: git.RecentSubjects(cfg.History),
		Style: cfg.Style, Language: cfg.Language, Scopes: cfg.Scopes,
		Instructions: cfg.Instructions, Hint: f.hint, MaxDiff: cfg.MaxDiff,
	}
	req := llm.Request{
		System: prompt.System(in), User: prompt.User(in),
		Model: cfg.Model, Temperature: cfg.Temperature, MaxTokens: 400,
	}

	if f.dryRun {
		fmt.Printf("provider: %s\nbase_url: %s\nmodel:    %s\n\n=== system ===\n%s\n=== user ===\n%s",
			cfg.Provider, cfg.BaseURL, cfg.Model, req.System, req.User)
		return nil
	}

	p, err := llm.New(llm.Options{
		Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey,
		Timeout: time.Duration(cfg.Timeout) * time.Second,
	})
	if err != nil {
		return err
	}
	if f.verbose {
		fmt.Fprintf(os.Stderr, "commitgen: %s @ %s (%d diff chars)\n", cfg.Model, cfg.BaseURL, len(diff))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	gen := func(n int) ([]string, error) { return generateN(ctx, p, req, n) }

	// Git hook mode: write the draft into the commit message file.
	if f.hook != "" {
		msgs, err := gen(1)
		if err != nil {
			fmt.Fprintln(os.Stderr, "commitgen: skipped —", err)
			return nil // never block a commit
		}
		existing, _ := os.ReadFile(f.hook)
		return os.WriteFile(f.hook, []byte(msgs[0]+"\n"+string(existing)), 0o644)
	}

	interactive := isTTY(os.Stdin) && isTTY(os.Stdout) && !f.print && !f.commit
	for {
		msgs, err := gen(max(1, f.n))
		if err != nil {
			return err
		}
		msg := msgs[0]
		if len(msgs) > 1 {
			if msg, err = pick(msgs); err != nil {
				return err
			}
		}

		if f.commit {
			return commit(msg, !f.yes)
		}
		if !interactive {
			fmt.Println(msg)
			return nil
		}
		if len(msgs) == 1 {
			fmt.Printf("\n%s\n\n", indent(msg))
		}
		switch ask("[c]ommit  [e]dit  [r]egenerate  [q]uit", "c") {
		case "c":
			return commit(msg, false)
		case "e":
			return commit(msg, true)
		case "r":
			continue
		default:
			return nil
		}
	}
}

// generateN runs n generations concurrently (slightly higher temperature for
// variety when n > 1) with a spinner on stderr.
func generateN(ctx context.Context, p llm.Provider, req llm.Request, n int) ([]string, error) {
	if n > 1 && req.Temperature < 0.7 {
		req.Temperature = 0.7
	}
	stop := spinner(fmt.Sprintf("generating with %s", req.Model))
	defer stop()

	out := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			raw, err := p.Generate(ctx, req, nil)
			out[i], errs[i] = prompt.Clean(raw), err
		}(i)
	}
	wg.Wait()

	var good []string
	seen := map[string]bool{}
	for i := range out {
		if errs[i] == nil && out[i] != "" && !seen[out[i]] {
			seen[out[i]] = true
			good = append(good, out[i])
		}
	}
	if len(good) == 0 {
		if errs[0] != nil {
			return nil, errs[0]
		}
		return nil, errors.New("the model returned an empty message")
	}
	return good, nil
}

func commit(msg string, edit bool) error {
	tmp, err := os.CreateTemp("", "commitgen-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(msg + "\n")
	tmp.Close()

	args := []string{"commit", "-F", tmp.Name()}
	if edit {
		args = append(args, "-e")
	}
	cmd := exec.Command("git", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Stop our own hook from regenerating on top of this message.
	cmd.Env = append(os.Environ(), "COMMITGEN_SKIP_HOOK=1")
	return cmd.Run()
}

func pick(msgs []string) (string, error) {
	fmt.Println()
	for i, m := range msgs {
		fmt.Printf("  %d) %s\n\n", i+1, strings.ReplaceAll(m, "\n", "\n     "))
	}
	if !isTTY(os.Stdin) {
		return msgs[0], nil
	}
	for {
		a := ask(fmt.Sprintf("pick 1-%d", len(msgs)), "1")
		if i, err := strconv.Atoi(a); err == nil && i >= 1 && i <= len(msgs) {
			return msgs[i-1], nil
		}
	}
}

func ask(q, def string) string {
	fmt.Printf("%s (%s): ", q, def)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def
	}
	return line[:1]
}

func indent(s string) string { return "  " + strings.ReplaceAll(s, "\n", "\n  ") }

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func spinner(label string) func() {
	if !isTTY(os.Stderr) {
		return func() {}
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		start := time.Now()
		for i := 0; ; i++ {
			select {
			case <-done:
				fmt.Fprint(os.Stderr, "\r\033[K")
				return
			case <-time.After(90 * time.Millisecond):
				fmt.Fprintf(os.Stderr, "\r%s %s… %.1fs", frames[i%len(frames)], label, time.Since(start).Seconds())
			}
		}
	}()
	return func() { close(done); wg.Wait() }
}

// ---------------- hook ----------------

const hookMarker = "# installed by commitgen"

const hookScript = `#!/bin/sh
` + hookMarker + `
# Drafts a commit message with commitgen when you run plain ` + "`git commit`" + `.
# Skipped for merges, amends, -m messages, templates, and commitgen's own commits.
[ -n "$COMMITGEN_SKIP_HOOK" ] && exit 0
case "$2" in merge|squash|commit|message|template) exit 0 ;; esac
command -v commitgen >/dev/null 2>&1 || exit 0
commitgen --hook "$1" </dev/null || true
`

func hookCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: commitgen hook install|uninstall [--force]")
	}
	gitDir, err := git.GitDir()
	if err != nil {
		return errors.New("not inside a git repository")
	}
	path := filepath.Join(gitDir, "hooks", "prepare-commit-msg")
	force := len(args) > 1 && args[1] == "--force"
	existing, readErr := os.ReadFile(path)
	ours := readErr == nil && strings.Contains(string(existing), hookMarker)

	switch args[0] {
	case "install":
		if readErr == nil && !ours && !force {
			return fmt.Errorf("%s already exists and was not created by commitgen (use --force to overwrite)", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(hookScript), 0o755); err != nil {
			return err
		}
		fmt.Println("installed", path)
		fmt.Println("now just run `git commit` — commitgen drafts the message in your editor.")
		return nil
	case "uninstall":
		if readErr != nil {
			fmt.Println("no hook installed")
			return nil
		}
		if !ours && !force {
			return fmt.Errorf("%s was not created by commitgen (use --force to remove)", path)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Println("removed", path)
		return nil
	}
	return fmt.Errorf("unknown hook command %q", args[0])
}

// ---------------- config ----------------

func configCmd(args []string) error {
	sub := "show"
	if len(args) > 0 {
		sub = args[0]
	}
	path := config.UserConfigPath()
	switch sub {
	case "path":
		fmt.Println(path)
	case "init":
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(config.Sample), 0o600); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	case "show":
		root, _ := git.RepoRoot()
		c, err := config.Load(root)
		if err != nil {
			return err
		}
		key := "(none)"
		if c.APIKey != "" {
			key = "(set)"
		}
		fmt.Printf("config file: %s\nprovider:    %s\nbase_url:    %s\nmodel:       %s\napi_key:     %s\nstyle:       %s\nlanguage:    %s\nmax_diff:    %d\nhistory:     %d\n",
			path, c.Provider, c.BaseURL, c.Model, key, c.Style, c.Language, c.MaxDiff, c.History)
	default:
		return fmt.Errorf("unknown config command %q", sub)
	}
	return nil
}
