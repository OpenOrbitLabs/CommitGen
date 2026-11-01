package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMergesFilesAndEnv(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.toml")
	os.WriteFile(user, []byte(`
model = "llama3.2"   # comment
max_diff = 5000
temperature = 0.5
exclude = ["*.snap", 'docs/*']
`), 0o644)
	repo := filepath.Join(dir, "repo")
	os.Mkdir(repo, 0o755)
	os.WriteFile(filepath.Join(repo, ".commitgen.toml"), []byte(`scopes = ["api", "ui"]
style = "simple"`), 0o644)

	t.Setenv("COMMITGEN_CONFIG", user)
	t.Setenv("COMMITGEN_BASE_URL", "http://example:1234/v1")

	c, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "llama3.2" || c.MaxDiff != 5000 || c.Temperature != 0.5 {
		t.Errorf("user file not applied: %+v", c)
	}
	if len(c.Exclude) != 2 || c.Exclude[1] != "docs/*" {
		t.Errorf("exclude = %v", c.Exclude)
	}
	if c.Style != "simple" || len(c.Scopes) != 2 {
		t.Errorf("repo file not applied: %+v", c)
	}
	if c.BaseURL != "http://example:1234/v1" {
		t.Errorf("env not applied: %s", c.BaseURL)
	}
}

func TestUnknownKey(t *testing.T) {
	f := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(f, []byte(`modle = "x"`), 0o644)
	t.Setenv("COMMITGEN_CONFIG", f)
	if _, err := Load(""); err == nil {
		t.Error("expected error for unknown key")
	}
}
