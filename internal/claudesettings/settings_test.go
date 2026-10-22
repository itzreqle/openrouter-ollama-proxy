package claudesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteModelPickerOnlyTouchesPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OROLLAMA_WRITE_CLAUDE_SETTINGS", "1")

	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	initial := []byte(`{
  "env": {
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "user-owned",
    "MY_CUSTOM": "keep-me"
  },
  "permissions": { "allow": ["Bash"] }
}
`)
	if err := os.WriteFile(path, initial, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteModelPicker([]Option{{Model: "openrouter-claude/a/b", Label: "A"}}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	env, ok := got["env"].(map[string]any)
	if !ok {
		t.Fatal("env block missing")
	}
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "user-owned" || env["MY_CUSTOM"] != "keep-me" {
		t.Fatalf("env mutated: %#v", env)
	}
	if _, ok := got["permissions"]; !ok {
		t.Fatal("permissions wiped")
	}
	mp, ok := got["modelPicker"].(map[string]any)
	if !ok {
		t.Fatal("modelPicker missing")
	}
	if mp["replaceBuiltInOptions"] != true {
		t.Fatalf("replaceBuiltInOptions: %#v", mp["replaceBuiltInOptions"])
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected .bak: %v", err)
	}
}

func TestWriteModelPickerRefusesCorruptJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OROLLAMA_WRITE_CLAUDE_SETTINGS", "1")

	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	corrupt := []byte(`{ not json`)
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteModelPicker([]Option{{Model: "x"}})
	if err == nil {
		t.Fatal("expected error")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != string(corrupt) {
		t.Fatalf("file was overwritten: %q", raw)
	}
}

func TestWriteModelPickerOptOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OROLLAMA_WRITE_CLAUDE_SETTINGS", "0")

	if err := WriteModelPicker([]Option{{Model: "x"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("should not write when opted out: %v", err)
	}
}
