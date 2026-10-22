// Package claudesettings owns writes to ~/.claude/settings.json modelPicker.
// Role env vars (ANTHROPIC_DEFAULT_*, ANTHROPIC_MODEL, discovery flags) are
// owned by the shell hook via resolved.env — this package must not mutate env.
package claudesettings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Option is one Claude Code modelPicker row.
type Option struct {
	Model       string `json:"model"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// Enabled reports whether settings writes are allowed.
// Default is on (1 / empty / true); set OROLLAMA_WRITE_CLAUDE_SETTINGS=0 to opt out.
func Enabled() bool {
	v := strings.TrimSpace(os.Getenv("OROLLAMA_WRITE_CLAUDE_SETTINGS"))
	if v == "" {
		return true
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		// treat "0" / "false" / "no" style; unknown → on
		switch strings.ToLower(v) {
		case "0", "off", "no", "false":
			return false
		default:
			return true
		}
	}
	return b
}

// WriteModelPicker merges modelPicker into ~/.claude/settings.json.
// On JSON parse failure it refuses to write (leaves the file untouched).
// Before overwriting a previously valid file it writes a .bak backup.
func WriteModelPicker(opts []Option) error {
	if !Enabled() {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	settings := map[string]any{}
	hadValidFile := false
	var previousGood []byte
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return fmt.Errorf("refuse to overwrite corrupt %s: %w", path, err)
		}
		hadValidFile = true
		previousGood = raw
	}

	clean := make([]Option, 0, len(opts))
	seen := map[string]struct{}{}
	for _, o := range opts {
		o.Model = strings.TrimSpace(o.Model)
		if o.Model == "" {
			continue
		}
		if _, ok := seen[o.Model]; ok {
			continue
		}
		seen[o.Model] = struct{}{}
		clean = append(clean, o)
	}

	settings["modelPicker"] = map[string]any{
		"replaceBuiltInOptions": true,
		"options":               clean,
	}
	// Do not touch settings["env"] — shell hook owns ANTHROPIC_* / discovery flags.

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	if hadValidFile {
		bak := path + ".bak"
		if err := os.WriteFile(bak, previousGood, 0o644); err != nil {
			return fmt.Errorf("backup settings before write: %w", err)
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
