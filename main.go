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

	"github.com/onetwothird/CommitGen/internal/config"
	"github.com/onetwothird/CommitGen/internal/git"
	"github.com/onetwothird/CommitGen/internal/llm"
	"github.com/onetwothird/CommitGen/internal/prompt"
	"github.com/onetwothird/CommitGen/internal/redact"
)

var version = "dev" 

const usage = `commitgen — write commit messages from your staged diff

Usage:
  commitgen [flags]            generate a message for the staged changes
  commitgen -a --each          commit every changed file separately, automatically
  commitgen hook install       draft messages automatically on ` + "`git commit`" + `
  commitgen hook uninstall
  commitgen config init        write a starter config file
  commitgen config path|show
  commitgen version

Flags:
`

type flags struct {
	commit, yes, print, dryRun, noRedact, verbose bool
	each, all                                     bool
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
	fs.BoolVar(&f.each, "each", false, "commit every staged file separately, each with its own message (automatic)")
	fs.BoolVar(&f.all, "a", false, "stage all changes first (like git add -A)")
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

type session struct {
	cfg config.Config
	f   flags
	p   llm.Provider
	ctx context.Context
}

func (s *session) request() (llm.Request, string, error) {
	diff, err := git.StagedDiff(s.cfg.Exclude)
	if err != nil {
		return llm.Request{}, "", err
	}
	stat, _ := git.StagedStat()
	if !s.f.noRedact {
		var n int
		diff, n = redact.Secrets(diff)
		if n > 0 {
			fmt.Fprintf(os.Stderr, "commitgen: masked %d possible secret(s) before sending\n", n)
		}
	}
	in := prompt.Input{
		Diff: diff, Stat: stat, Branch: git.Branch(), History: git.RecentSubjects(s.cfg.History),
		Style: s.cfg.Style, Language: s.cfg.Language, Scopes: s.cfg.Scopes,
		Instructions: s.cfg.Instructions, Hint: s.f.hint, MaxDiff: s.cfg.MaxDiff,
	}
	if s.f.verbose {
		fmt.Fprintf(os.Stderr, "commitgen: %s @ %s (%d diff chars)\n", s.cfg.Model, s.cfg.BaseURL, len(diff))
	}
	return llm.Request{
		System: prompt.System(in), User: prompt.User(in),
		Model: s.cfg.Model, Temperature: s.cfg.Temperature, MaxTokens: 400,
	}, stat, nil
}

func (s *session) generate(req llm.Request, stat string, n int) ([]string, error) {
	msgs, err := generateN(s.ctx, s.p, req, n)
	if err != nil {
		return nil, err
	}
	for i, m := range msgs {
		if prompt.Valid(m, s.cfg.Style) {
			continue
		}
		if s.f.verbose {
			fmt.Fprintf(os.Stderr, "commitgen: model reply was not a commit message, asking it to rewrite:\n%s\n", m)
		}
		fix := req
		fix.User = prompt.RepairUser(m, stat, s.cfg.Style)
		if fixed, err := generateN(s.ctx, s.p, fix, 1); err == nil && prompt.Valid(fixed[0], s.cfg.Style) {
			msgs[i] = fixed[0]
		} else {
			fmt.Fprintln(os.Stderr, "commitgen: warning: the model's reply doesn't look like a commit message.\n"+
				"  Tips: stage fewer files at a time, add context with -m \"...\", or try a larger model (--model).")
		}
	}
	return msgs, nil
}

func newSession(f flags) (*session, context.CancelFunc, error) {
	cfg, _, err := loadConfig(f)
	if err != nil {
		return nil, nil, err
	}
	p, err := llm.New(llm.Options{
		Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey,
		Timeout: time.Duration(cfg.Timeout) * time.Second, NumCtx: cfg.NumCtx,
	})
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	return &session{cfg: cfg, f: f, p: p, ctx: ctx}, cancel, nil
}

func generateCmd(f flags) error {
	if f.all {
		if err := git.StageAll(); err != nil {
			return err
		}
	}
	s, cancel, err := newSession(f)
	if err != nil {
		return err
	}
	defer cancel()
	if f.each {
		return eachCmd(s)
	}
	cfg := s.cfg

	req, stat, err := s.request()
	if err != nil {
		if f.hook != "" && errors.Is(err, git.ErrNothingStaged) {
			return nil 
		}
		return err
	}

	if f.dryRun {
		fmt.Printf("provider: %s\nbase_url: %s\nmodel:    %s\n\n=== system ===\n%s\n=== user ===\n%s",
			cfg.Provider, cfg.BaseURL, cfg.Model, req.System, req.User)
		return nil
	}

	gen := func(n int) ([]string, error) { return s.generate(req, stat, n) }

	if f.hook != "" {
		msgs, err := gen(1)
		if err != nil {
			fmt.Fprintln(os.Stderr, "commitgen: skipped —", err)
			return nil
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

func eachCmd(s *session) error {
	if root, err := git.RepoRoot(); err == nil {
		os.Chdir(root)
	}
	files, err := git.StagedFiles()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return git.ErrNothingStaged
	}
	snapshot, err := git.WriteTree()
	if err != nil {
		return err
	}
	restore := func() {
		git.RestoreIndex(snapshot)
		git.RefreshIndex()
	}
	preview := s.f.dryRun || s.f.print
	if preview {
		defer restore()
	}

	fmt.Fprintf(os.Stderr, "commitgen: %d file(s) staged, committing one by one\n", len(files))
	done := 0
	for i, file := range files {
		if err := git.ResetIndex(); err != nil {
			restore()
			return err
		}
		if err := git.StageFromTree(snapshot, file); err != nil {
			restore()
			return err
		}
		req, stat, err := s.request()
		if err == nil {
			var msgs []string
			msgs, err = s.generate(req, stat, 1)
			if err == nil {
				fmt.Printf("\n[%d/%d] %s\n%s\n", i+1, len(files), file, indent(msgs[0]))
				if !preview {
					err = commitQuiet(msgs[0])
				}
			}
		}
		if err != nil {
			restore()
			return fmt.Errorf("stopped at %s after %d commit(s): %w\n  the remaining files are still staged", file, done, err)
		}
		done++
	}
	git.RefreshIndex()
	if preview {
		fmt.Fprintf(os.Stderr, "\ncommitgen: preview only, nothing was committed\n")
	} else {
		fmt.Fprintf(os.Stderr, "\ncommitgen: created %d commit(s)\n", done)
	}
	return nil
}

func commitQuiet(msg string) error {
	tmp, err := os.CreateTemp("", "commitgen-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	tmp.WriteString(msg + "\n")
	tmp.Close()
	cmd := exec.Command("git", "commit", "-q", "-F", tmp.Name())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "COMMITGEN_SKIP_HOOK=1")
	return cmd.Run()
}
