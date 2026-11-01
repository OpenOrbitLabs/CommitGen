// Package config loads commitgen settings from (lowest to highest priority):
// built-in defaults, the user config file, a per-repo .commitgen.toml,
// and environment variables. CLI flags are applied on top by main.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Provider     string   // "openai" (any OpenAI-compatible API, incl. Ollama) or "anthropic"
	BaseURL      string   // e.g. http://localhost:11434/v1
	Model        string   // e.g. qwen2.5-coder:7b
	APIKey       string   // read from env or file; never required for local models
	MaxDiff      int      // max diff characters sent to the model
	Temperature  float64  // sampling temperature
	Style        string   // "conventional" or "simple"
	Language     string   // language of the commit message, e.g. "English"
	Exclude      []string // extra pathspecs to exclude from the diff
	Scopes       []string // allowed scopes (optional, per-repo)
	Instructions string   // extra free-form instructions for the model
	History      int      // how many recent commit subjects to include for style matching
	Timeout      int      // HTTP timeout in seconds
	NumCtx       int      // Ollama context window in tokens
}

func Defaults() Config {
	return Config{
		Provider:    "openai",
		BaseURL:     "http://localhost:11434/v1",
		Model:       "qwen2.5-coder:7b",
		MaxDiff:     12000,
		Temperature: 0.2,
		Style:       "conventional",
		Language:    "English",
		History:     10,
		Timeout:     120,
		NumCtx:      8192,
	}
}

// UserConfigPath returns ~/.config/commitgen/config.toml (or the OS equivalent).
func UserConfigPath() string {
	if p := os.Getenv("COMMITGEN_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "commitgen", "config.toml")
}

// Load builds the effective config. repoRoot may be empty.
func Load(repoRoot string) (Config, error) {
	c := Defaults()
	if err := c.mergeFile(UserConfigPath()); err != nil {
		return c, err
	}
	if repoRoot != "" {
		if err := c.mergeFile(filepath.Join(repoRoot, ".commitgen.toml")); err != nil {
			return c, err
		}
	}
	c.mergeEnv()
	return c, nil
}

func (c *Config) mergeEnv() {
	set := func(k string, dst *string) {
		if v := os.Getenv(k); v != "" {
			*dst = v
		}
	}
	set("COMMITGEN_PROVIDER", &c.Provider)
	set("COMMITGEN_BASE_URL", &c.BaseURL)
	set("COMMITGEN_MODEL", &c.Model)
	set("COMMITGEN_API_KEY", &c.APIKey)
	set("COMMITGEN_STYLE", &c.Style)
	set("COMMITGEN_LANGUAGE", &c.Language)
	// Fall back to well-known provider keys.
	if c.APIKey == "" {
		switch c.Provider {
		case "anthropic":
			c.APIKey = os.Getenv("ANTHROPIC_API_KEY")
		default:
			if strings.Contains(c.BaseURL, "openai.com") {
				c.APIKey = os.Getenv("OPENAI_API_KEY")
			}
		}
	}
}

func (c *Config) mergeFile(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	kv, err := parseTOML(f)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for k, v := range kv {
		if err := c.apply(k, v); err != nil {
			return fmt.Errorf("%s: %s: %w", path, k, err)
		}
	}
	return nil
}

func (c *Config) apply(key string, v value) error {
	var err error
	switch key {
	case "provider":
		c.Provider, err = v.str()
	case "base_url":
		c.BaseURL, err = v.str()
	case "model":
		c.Model, err = v.str()
	case "api_key":
		c.APIKey, err = v.str()
	case "style":
		c.Style, err = v.str()
	case "language":
		c.Language, err = v.str()
	case "instructions":
		c.Instructions, err = v.str()
	case "max_diff":
		c.MaxDiff, err = v.int()
	case "history":
		c.History, err = v.int()
	case "timeout":
		c.Timeout, err = v.int()
	case "num_ctx":
		c.NumCtx, err = v.int()
	case "temperature":
		c.Temperature, err = v.float()
	case "exclude":
		c.Exclude, err = v.list()
	case "scopes":
		c.Scopes, err = v.list()
	default:
		return fmt.Errorf("unknown key")
	}
	return err
}

// ---- minimal TOML subset: `key = "string" | number | true/false | ["a", "b"]` ----

type value struct {
	raw   string
	arr   []string
	isArr bool
}

func (v value) str() (string, error) {
	if v.isArr {
		return "", fmt.Errorf("expected string, got array")
	}
	return unquote(v.raw)
}
func (v value) int() (int, error)       { return strconv.Atoi(v.raw) }
func (v value) float() (float64, error) { return strconv.ParseFloat(v.raw, 64) }
func (v value) list() ([]string, error) {
	if !v.isArr {
		return nil, fmt.Errorf("expected array")
	}
	return v.arr, nil
}

func unquote(s string) (string, error) {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"') {
		return strconv.Unquote(s)
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1], nil
	}
	return "", fmt.Errorf("expected quoted string, got %s", s)
}

func stripComment(line string) string {
	inStr := byte(0)
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case inStr != 0 && ch == '\\' && inStr == '"':
			i++
		case inStr != 0 && ch == inStr:
			inStr = 0
		case inStr == 0 && (ch == '"' || ch == '\''):
			inStr = ch
		case inStr == 0 && ch == '#':
			return line[:i]
		}
	}
	return line
}

func parseTOML(f *os.File) (map[string]value, error) {
	out := map[string]value{}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(stripComment(sc.Text()))
		if line == "" || strings.HasPrefix(line, "[") && !strings.Contains(line, "=") {
			continue // blank line or [table] header (ignored, flat keys only)
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value", n)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if strings.HasPrefix(v, "[") {
			if !strings.HasSuffix(v, "]") {
				return nil, fmt.Errorf("line %d: arrays must be on one line", n)
			}
			var items []string
			for _, p := range splitArray(v[1 : len(v)-1]) {
				s, err := unquote(strings.TrimSpace(p))
				if err != nil {
					return nil, fmt.Errorf("line %d: %w", n, err)
				}
				items = append(items, s)
			}
			out[k] = value{arr: items, isArr: true}
			continue
		}
		out[k] = value{raw: v}
	}
	return out, sc.Err()
}

func splitArray(s string) []string {
	var parts []string
	var cur strings.Builder
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr == 0 && ch == ',' {
			if t := strings.TrimSpace(cur.String()); t != "" {
				parts = append(parts, t)
			}
			cur.Reset()
			continue
		}
		if inStr == 0 && (ch == '"' || ch == '\'') {
			inStr = ch
		} else if inStr == ch {
			inStr = 0
		}
		cur.WriteByte(ch)
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		parts = append(parts, t)
	}
	return parts
}

const Sample = `# commitgen config
# Location: ` + "`commitgen config path`" + ` prints it. Per-repo overrides go in .commitgen.toml.

# Any OpenAI-compatible API. Default is a local Ollama server (private, free).
provider = "openai"
base_url = "http://localhost:11434/v1"
model    = "qwen2.5-coder:7b"

# Cloud examples:
# base_url = "https://api.openai.com/v1"      # model = "gpt-4o-mini", key from OPENAI_API_KEY
# base_url = "https://api.groq.com/openai/v1" # model = "llama-3.1-8b-instant"
# provider = "anthropic"                      # model = "claude-haiku-4-5", key from ANTHROPIC_API_KEY

style       = "conventional"   # or "simple"
language    = "English"
max_diff    = 12000            # characters of diff sent to the model
history     = 10               # recent commit subjects used to match repo style
temperature = 0.2
timeout     = 120
num_ctx     = 8192             # Ollama context window; raise it for big diffs if you have RAM

# exclude = ["docs/generated/*", "*.snap"]
# scopes  = ["api", "ui", "cli"]
# instructions = "Mention the ticket ID from the branch name if present."
`
