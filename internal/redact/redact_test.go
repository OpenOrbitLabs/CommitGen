package redact

import (
	"strings"
	"testing"
)

func TestSecrets(t *testing.T) {
	in := `+OPENAI_API_KEY=sk-proj-abcdefghijklmnopqrstuvwxyz123456
+aws = "AKIAIOSFODNN7EXAMPLE"
+"password": "hunter2hunter2"
+token: ghp_abcdefghijklmnopqrstuvwxyz0123456789
+name = "commitgen"`
	out, n := Secrets(in)
	for _, leak := range []string{"sk-proj-abc", "AKIAIOSFODNN7EXAMPLE", "hunter2", "ghp_abc"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %q:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, `name = "commitgen"`) {
		t.Error("redacted a harmless line")
	}
	if n < 4 {
		t.Errorf("expected >=4 redactions, got %d", n)
	}
}
