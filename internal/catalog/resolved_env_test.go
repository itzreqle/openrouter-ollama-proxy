package catalog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "''"},
		{"simple", "'simple'"},
		{"openrouter-claude/a/b:free", "'openrouter-claude/a/b:free'"},
		{"it's", "'it'\\''s'"},
		{"a'b'c", "'a'\\''b'\\''c'"},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.in); got != tc.want {
			t.Fatalf("shellQuote(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
	// Round-trip through bash for the quote-escape form.
	cmd := exec.Command("bash", "-c", `v=`+shellQuote("it's")+`; printf '%s' "$v"`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(out))
	}
	if string(out) != "it's" {
		t.Fatalf("bash round-trip: %q", out)
	}
}

func TestWriteResolvedEnvQuotedAndSafeToSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	malicious := "evil';x; echo pwned #"
	c := &Catalog{}
	c.mu.Lock()
	c.resolved = Resolved{
		Opus:    malicious,
		Sonnet:  "openrouter-claude/ok/model:free",
		Haiku:   "it's-fine",
		Default: "openrouter-claude/ok/model:free",
	}
	c.mu.Unlock()

	if err := c.WriteResolvedEnv(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".cache", "orollama", "resolved.env")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	wantLine := "OROLLAMA_MODEL_OPUS=" + shellQuote(malicious)
	if !strings.Contains(body, wantLine) {
		t.Fatalf("expected %q in:\n%s", wantLine, body)
	}
	if strings.Contains(body, "\nevil=") {
		t.Fatal("injection created a second assignment line")
	}

	// Bash must source without treating the payload as shell code.
	cmd := exec.Command("bash", "-c", `set -a; source "$1"; set +a; printf '%s' "$OROLLAMA_MODEL_OPUS"`, "bash", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("source failed: %v\n%s", err, out)
	}
	if string(out) != malicious {
		t.Fatalf("sourced value = %q want %q", out, malicious)
	}
}