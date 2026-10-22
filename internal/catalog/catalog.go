package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/config"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/model"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/openrouter"
)

// Entry is a ranked OpenRouter model ready for listing / aliases.
type Entry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	Free          bool   `json:"free"`
	PrefixedID    string `json:"prefixed_id"` // openrouter-claude/<id> (Claude Code discovery-friendly)
	SupportsTools bool   `json:"supports_tools,omitempty"`
}

// Resolved holds Claude role mappings after override/auto resolution.
type Resolved struct {
	Opus      string
	Sonnet    string
	Haiku     string
	Default   string
	OpusAuto  bool
	SonnetAuto bool
	HaikuAuto bool
	OpusCtx   int
	SonnetCtx int
	HaikuCtx  int
}

// Catalog caches ranked OpenRouter models and resolves Claude aliases.
type Catalog struct {
	cfg    config.Config
	client *openrouter.Client

	mu        sync.RWMutex
	entries   []Entry
	updatedAt time.Time
	ttl       time.Duration
	resolved  Resolved
}

// New builds a catalog manager.
func New(cfg config.Config, client *openrouter.Client) *Catalog {
	ttl := time.Duration(cfg.CatalogCacheSeconds) * time.Second
	if ttl < 30*time.Second {
		ttl = 10 * time.Minute
	}
	c := &Catalog{
		cfg:    cfg,
		client: client,
		ttl:    ttl,
	}
	c.resolved = c.resolveFromOverrides(nil)
	return c
}

// Stale reports whether a refresh is due.
func (c *Catalog) Stale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.updatedAt.IsZero() {
		return true
	}
	return time.Since(c.updatedAt) > c.ttl
}

// Entries returns a copy of the ranked catalog.
func (c *Catalog) Entries() []Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Entry, len(c.entries))
	copy(out, c.entries)
	return out
}

// Resolved returns current role mappings.
func (c *Catalog) Resolved() Resolved {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resolved
}

// ApplyToRouter updates router aliases from the latest resolution.
func (c *Catalog) ApplyToRouter(r *model.Router) {
	res := c.Resolved()
	r.Aliases = model.Aliases{
		Opus:    res.Opus,
		Sonnet:  res.Sonnet,
		Haiku:   res.Haiku,
		Default: res.Default,
	}
}

// Refresh fetches OpenRouter models and updates aliases + resolved.env.
func (c *Catalog) Refresh(ctx context.Context) error {
	if c.client == nil || c.cfg.OpenRouterAPIKey == "" {
		c.mu.Lock()
		c.resolved = c.resolveFromOverrides(nil)
		c.mu.Unlock()
		_ = c.WriteResolvedEnv()
		return fmt.Errorf("openrouter unavailable")
	}

	// Full free/text catalog for /v1/models (Claude Code /model picker).
	// ToolsOnly is applied only when auto-picking Opus/Sonnet/Haiku roles.
	models, err := c.client.ListModelsRanked(ctx, openrouter.ListOptions{
		Sort:      c.cfg.OpenRouterSort,
		ToolsOnly: false,
		FreeOnly:  c.cfg.OpenRouterFreeOnly,
	})
	if err != nil {
		return err
	}

	entries := make([]Entry, 0, len(models))
	for _, m := range models {
		free := strings.Contains(m.ID, ":free") || isZeroPriced(m)
		entries = append(entries, Entry{
			ID:            m.ID,
			Name:          m.Name,
			ContextLength: m.ContextLength,
			Free:          free,
			PrefixedID:    "openrouter-claude/" + m.ID,
			SupportsTools: supportsTools(m),
		})
	}

	pickFrom := entries
	if c.cfg.OpenRouterToolsOnly {
		if tools := filterTools(entries); len(tools) > 0 {
			pickFrom = tools
		}
	}
	resolved := c.resolveFromOverrides(pickFrom)

	c.mu.Lock()
	c.entries = entries
	c.resolved = resolved
	c.updatedAt = time.Now()
	c.mu.Unlock()

	if err := c.WriteResolvedEnv(); err != nil {
		return err
	}
	// Claude Code's /model UI often keeps only the 5 role rows even when
	// gateway discovery succeeds; modelPicker forces the full catalog in.
	_ = c.WriteClaudeSettings(nil)
	return nil
}

func (c *Catalog) resolveFromOverrides(entries []Entry) Resolved {
	pick := func(i int) (string, int) {
		if i < 0 || i >= len(entries) {
			return "", 0
		}
		return entries[i].PrefixedID, entries[i].ContextLength
	}

	auto0, ctx0 := pick(0)
	auto1, ctx1 := pick(1)
	auto2, ctx2 := pick(2)

	res := Resolved{}

	if c.cfg.ModelOpus != "" {
		res.Opus = c.cfg.ModelOpus
		res.OpusCtx = contextForPrefixed(entries, c.cfg.ModelOpus)
	} else if auto0 != "" {
		res.Opus = auto0
		res.OpusCtx = ctx0
		res.OpusAuto = true
	}

	if c.cfg.ModelSonnet != "" {
		res.Sonnet = c.cfg.ModelSonnet
		res.SonnetCtx = contextForPrefixed(entries, c.cfg.ModelSonnet)
	} else if auto1 != "" {
		res.Sonnet = auto1
		res.SonnetCtx = ctx1
		res.SonnetAuto = true
	} else if auto0 != "" {
		res.Sonnet = auto0
		res.SonnetCtx = ctx0
		res.SonnetAuto = true
	}

	if c.cfg.ModelHaiku != "" {
		res.Haiku = c.cfg.ModelHaiku
		res.HaikuCtx = contextForPrefixed(entries, c.cfg.ModelHaiku)
	} else if auto2 != "" {
		res.Haiku = auto2
		res.HaikuCtx = ctx2
		res.HaikuAuto = true
	} else if auto1 != "" {
		res.Haiku = auto1
		res.HaikuCtx = ctx1
		res.HaikuAuto = true
	} else if auto0 != "" {
		res.Haiku = auto0
		res.HaikuCtx = ctx0
		res.HaikuAuto = true
	}

	if c.cfg.Model != "" {
		res.Default = c.cfg.Model
	} else if res.Sonnet != "" {
		res.Default = res.Sonnet
	} else {
		res.Default = res.Opus
	}

	return res
}

func supportsTools(m openrouter.Model) bool {
	for _, p := range m.SupportedParameters {
		if strings.EqualFold(p, "tools") {
			return true
		}
	}
	return false
}

func isZeroPriced(m openrouter.Model) bool {
	if m.Pricing == nil {
		return false
	}
	return zeroPrice(m.Pricing.Prompt) && zeroPrice(m.Pricing.Completion)
}

func zeroPrice(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s == "0"
	}
	return f == 0
}

func filterTools(entries []Entry) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.SupportsTools {
			out = append(out, e)
		}
	}
	return out
}

func contextForPrefixed(entries []Entry, prefixed string) int {
	prefixed = model.StripContextSuffix(prefixed)
	bare := prefixed
	for _, p := range []string{
		"openrouter-claude/", "openrouter-claude:",
		"openrouter/", "openrouter:",
	} {
		if strings.HasPrefix(strings.ToLower(bare), strings.ToLower(p)) {
			bare = bare[len(p):]
			break
		}
	}
	for _, e := range entries {
		if e.PrefixedID == prefixed || e.ID == bare {
			return e.ContextLength
		}
	}
	return 0
}

// CacheDir returns ~/.cache/orollama.
func CacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".cache", "orollama")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// PickerOption is one Claude Code modelPicker row.
type PickerOption struct {
	Model       string `json:"model"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// WriteResolvedEnv writes ~/.cache/orollama/resolved.env for shell hooks.
func (c *Catalog) WriteResolvedEnv() error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	res := c.Resolved()
	ctx := res.SonnetCtx
	if ctx <= 0 {
		ctx = res.OpusCtx
	}
	if ctx <= 0 {
		ctx = 200000
	}

	var b strings.Builder
	b.WriteString("# generated by orollama — do not edit\n")
	fmt.Fprintf(&b, "OROLLAMA_MODEL_OPUS=%s\n", res.Opus)
	fmt.Fprintf(&b, "OROLLAMA_MODEL_SONNET=%s\n", res.Sonnet)
	fmt.Fprintf(&b, "OROLLAMA_MODEL_HAIKU=%s\n", res.Haiku)
	fmt.Fprintf(&b, "OROLLAMA_MODEL_DEFAULT=%s\n", res.Default)
	fmt.Fprintf(&b, "OROLLAMA_OPUS_AUTO=%t\n", res.OpusAuto)
	fmt.Fprintf(&b, "OROLLAMA_SONNET_AUTO=%t\n", res.SonnetAuto)
	fmt.Fprintf(&b, "OROLLAMA_HAIKU_AUTO=%t\n", res.HaikuAuto)
	// Also emit ANTHROPIC_* so long-lived shells that only source this file
	// still get discovery-friendly ids (not stale openrouter/ pins).
	if res.Opus != "" {
		fmt.Fprintf(&b, "ANTHROPIC_DEFAULT_OPUS_MODEL=%s\n", res.Opus)
	}
	if res.Sonnet != "" {
		fmt.Fprintf(&b, "ANTHROPIC_DEFAULT_SONNET_MODEL=%s\n", res.Sonnet)
		fmt.Fprintf(&b, "ANTHROPIC_MODEL=%s\n", res.Sonnet)
	}
	if res.Haiku != "" {
		fmt.Fprintf(&b, "ANTHROPIC_DEFAULT_HAIKU_MODEL=%s\n", res.Haiku)
	}
	fmt.Fprintf(&b, "CLAUDE_CODE_MAX_CONTEXT_TOKENS=%d\n", ctx)
	fmt.Fprintf(&b, "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1\n")
	fmt.Fprintf(&b, "CLAUDE_CODE_GATEWAY_MODEL_DISCOVERY_TIMEOUT_MS=15000\n")

	path := filepath.Join(dir, "resolved.env")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WriteClaudeSettings merges gateway modelPicker + env into ~/.claude/settings.json.
// extra is typically local ollama-claude/* rows.
func (c *Catalog) WriteClaudeSettings(extra []PickerOption) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	settings := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &settings); err != nil {
			settings = map[string]any{}
		}
	}

	opts := make([]PickerOption, 0, len(extra)+len(c.Entries())+3)
	seen := map[string]struct{}{}
	add := func(o PickerOption) {
		o.Model = strings.TrimSpace(o.Model)
		if o.Model == "" {
			return
		}
		if _, ok := seen[o.Model]; ok {
			return
		}
		seen[o.Model] = struct{}{}
		opts = append(opts, o)
	}

	res := c.Resolved()
	roleOf := map[string]string{}
	if res.Opus != "" {
		roleOf[res.Opus] = "Opus"
	}
	if res.Sonnet != "" {
		roleOf[res.Sonnet] = "Sonnet"
	}
	if res.Haiku != "" {
		roleOf[res.Haiku] = "Haiku"
	}

	// OpenRouter first, same order as ListModelsRanked (intelligence-high-to-low).
	for _, e := range c.Entries() {
		label := e.Name
		if label == "" {
			label = e.ID
		}
		desc := "OpenRouter via orollama"
		if e.Free {
			desc = "OpenRouter :free via orollama"
		}
		if e.ContextLength > 0 {
			desc = fmt.Sprintf("%s · %dk context", desc, e.ContextLength/1000)
		}
		if role, ok := roleOf[e.PrefixedID]; ok {
			desc = fmt.Sprintf("%s · auto %s", desc, role)
		}
		add(PickerOption{Model: e.PrefixedID, Label: label, Description: desc})
	}
	// Local Ollama after the ranked remote catalog.
	for _, e := range extra {
		add(e)
	}

	settings["modelPicker"] = map[string]any{
		// Keep Default row; replace Anthropic Fable/family chrome with our catalog.
		"replaceBuiltInOptions": true,
		"options":               opts,
	}

	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	env["CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"] = "1"
	env["CLAUDE_CODE_GATEWAY_MODEL_DISCOVERY_TIMEOUT_MS"] = "15000"
	if res.Opus != "" {
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = res.Opus
	}
	if res.Sonnet != "" {
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = res.Sonnet
		env["ANTHROPIC_MODEL"] = res.Sonnet
	}
	if res.Haiku != "" {
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = res.Haiku
	}
	ctxTok := res.SonnetCtx
	if ctxTok <= 0 {
		ctxTok = res.OpusCtx
	}
	if ctxTok <= 0 {
		ctxTok = 1000000
	}
	env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = strconv.Itoa(ctxTok)
	settings["env"] = env

	// Drop a stale "haiku"/"opus" alias default so Claude doesn't ignore gateway pins.
	if m, ok := settings["model"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(m)) {
		case "haiku", "opus", "sonnet", "fable", "best", "default":
			delete(settings, "model")
		}
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ResolvedEnvPath is the path shell hooks should source.
func ResolvedEnvPath() string {
	dir, err := CacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "resolved.env")
}
