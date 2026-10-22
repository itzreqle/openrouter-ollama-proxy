package model

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Backend identifies which upstream should handle a request.
type Backend string

const (
	BackendOpenRouter Backend = "openrouter"
	BackendOllama     Backend = "ollama"
)

// Route is the resolved destination for a model string.
type Route struct {
	Backend  Backend
	Upstream string // model id sent to the upstream
	Original string
}

// Aliases maps Claude Code role names to configured model ids.
type Aliases struct {
	Opus    string
	Sonnet  string
	Haiku   string
	Default string
}

// TagCache holds a periodically refreshed set of local Ollama model names.
type TagCache struct {
	mu        sync.RWMutex
	names     map[string]struct{}
	updatedAt time.Time
	ttl       time.Duration
}

// NewTagCache creates an empty tag cache with the given TTL.
func NewTagCache(ttl time.Duration) *TagCache {
	if ttl < time.Second {
		ttl = 30 * time.Second
	}
	return &TagCache{
		names: make(map[string]struct{}),
		ttl:   ttl,
	}
}

// Set replaces the cached name set.
func (c *TagCache) Set(names []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := make(map[string]struct{}, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		next[n] = struct{}{}
		// also index without :latest
		if base, ok := strings.CutSuffix(n, ":latest"); ok {
			next[base] = struct{}{}
		}
	}
	c.names = next
	c.updatedAt = time.Now()
}

// Has reports whether name is in the cache.
func (c *TagCache) Has(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.names[name]
	if ok {
		return true
	}
	_, ok = c.names[name+":latest"]
	return ok
}

// Names returns a copy of cached names.
func (c *TagCache) Names() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.names))
	for n := range c.names {
		out = append(out, n)
	}
	return out
}

// Stale reports whether the cache should be refreshed.
func (c *TagCache) Stale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.updatedAt.IsZero() {
		return true
	}
	return time.Since(c.updatedAt) > c.ttl
}

// Router resolves model strings to upstream backends.
type Router struct {
	aliases  atomic.Pointer[Aliases]
	TagCache *TagCache
}

// SetAliases stores role mappings atomically (safe under concurrent Resolve).
func (r *Router) SetAliases(a Aliases) {
	cp := a
	r.aliases.Store(&cp)
}

// GetAliases returns a copy of the current role mappings.
func (r *Router) GetAliases() Aliases {
	if p := r.aliases.Load(); p != nil {
		return *p
	}
	return Aliases{}
}

// Resolve applies prefix rules, aliases, and local tag lookup.
func (r *Router) Resolve(raw string) (Route, error) {
	original := strings.TrimSpace(raw)
	if original == "" {
		return Route{}, ErrEmptyModel
	}

	name := StripContextSuffix(original)

	// Claude Code / Anthropic default model names → env aliases
	if mapped := r.applyAlias(name); mapped != "" {
		name = mapped
	}

	// Discovery-friendly prefixes first (contain "claude" for Claude Code /v1/models filter).
	if rest, ok := cutPrefix(name, "ollama-claude/", "ollama-claude:"); ok {
		if rest == "" {
			return Route{}, ErrEmptyModel
		}
		return Route{Backend: BackendOllama, Upstream: rest, Original: original}, nil
	}
	if rest, ok := cutPrefix(name, "openrouter-claude/", "openrouter-claude:"); ok {
		if rest == "" {
			return Route{}, ErrEmptyModel
		}
		return Route{Backend: BackendOpenRouter, Upstream: rest, Original: original}, nil
	}

	if rest, ok := cutPrefix(name, "ollama/", "ollama:"); ok {
		if rest == "" {
			return Route{}, ErrEmptyModel
		}
		return Route{Backend: BackendOllama, Upstream: rest, Original: original}, nil
	}

	if rest, ok := cutPrefix(name, "openrouter/", "openrouter:"); ok {
		if rest == "" {
			return Route{}, ErrEmptyModel
		}
		return Route{Backend: BackendOpenRouter, Upstream: rest, Original: original}, nil
	}

	// vendor/model style → OpenRouter
	if strings.Contains(name, "/") {
		return Route{Backend: BackendOpenRouter, Upstream: name, Original: original}, nil
	}

	// bare name present in Ollama → local
	if r.TagCache != nil && r.TagCache.Has(name) {
		return Route{Backend: BackendOllama, Upstream: name, Original: original}, nil
	}

	return Route{}, &UnknownModelError{Model: original}
}

// StripContextSuffix removes Claude Code display suffixes like [1m] / [1M].
func StripContextSuffix(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndex(name, "["); i >= 0 && strings.HasSuffix(name, "]") {
		inner := name[i+1 : len(name)-1]
		if strings.EqualFold(inner, "1m") {
			return strings.TrimSpace(name[:i])
		}
	}
	return name
}

func (r *Router) applyAlias(name string) string {
	lower := strings.ToLower(name)
	aliases := r.GetAliases()

	// Never rewrite explicitly routed or vendor/model OpenRouter ids.
	// (Anthropic-style claude-* ids may still contain '/', but those are allowlisted below.)
	if hasRoutingPrefix(lower) {
		return ""
	}
	if strings.Contains(lower, "/") && !isAnthropicStyleID(lower) {
		return ""
	}

	switch {
	case isOpusAlias(lower):
		return aliases.Opus
	case isSonnetAlias(lower):
		return aliases.Sonnet
	case isHaikuAlias(lower):
		return aliases.Haiku
	case isFableAlias(lower):
		// Fable maps to Opus slot when pinned; otherwise default.
		if aliases.Opus != "" {
			return aliases.Opus
		}
		return aliases.Default
	case lower == "default" || strings.HasPrefix(lower, "claude-") || lower == "claude":
		return aliases.Default
	}
	return ""
}

func hasRoutingPrefix(lower string) bool {
	return strings.HasPrefix(lower, "ollama-claude/") || strings.HasPrefix(lower, "ollama-claude:") ||
		strings.HasPrefix(lower, "openrouter-claude/") || strings.HasPrefix(lower, "openrouter-claude:") ||
		strings.HasPrefix(lower, "ollama/") || strings.HasPrefix(lower, "ollama:") ||
		strings.HasPrefix(lower, "openrouter/") || strings.HasPrefix(lower, "openrouter:")
}

func isAnthropicStyleID(lower string) bool {
	// anthropic/claude-… or bare claude-…
	if strings.HasPrefix(lower, "anthropic/") {
		return true
	}
	return strings.HasPrefix(lower, "claude-") || lower == "claude"
}

func isOpusAlias(lower string) bool {
	switch lower {
	case "opus", "opusplan", "opus[1m]", "best":
		return true
	}
	if strings.HasPrefix(lower, "claude-opus") || strings.HasPrefix(lower, "claude-3-opus") {
		return true
	}
	return strings.HasPrefix(lower, "anthropic/claude-opus")
}

func isSonnetAlias(lower string) bool {
	switch lower {
	case "sonnet", "sonnet[1m]":
		return true
	}
	if strings.HasPrefix(lower, "claude-sonnet") || strings.HasPrefix(lower, "claude-3-5-sonnet") ||
		strings.HasPrefix(lower, "claude-3-7-sonnet") || strings.HasPrefix(lower, "claude-3-sonnet") {
		return true
	}
	return strings.HasPrefix(lower, "anthropic/claude-sonnet") || strings.HasPrefix(lower, "anthropic/claude-3-5-sonnet") ||
		strings.HasPrefix(lower, "anthropic/claude-3-7-sonnet")
}

func isHaikuAlias(lower string) bool {
	switch lower {
	case "haiku":
		return true
	}
	if strings.HasPrefix(lower, "claude-haiku") || strings.HasPrefix(lower, "claude-3-5-haiku") ||
		strings.HasPrefix(lower, "claude-3-haiku") {
		return true
	}
	if strings.HasPrefix(lower, "anthropic/claude-haiku") {
		return true
	}
	return strings.HasPrefix(lower, "anthropic/") && strings.Contains(lower, "haiku")
}

func isFableAlias(lower string) bool {
	switch lower {
	case "fable", "fable[1m]":
		return true
	}
	return strings.HasPrefix(lower, "claude-fable") || strings.HasPrefix(lower, "anthropic/claude-fable")
}

func cutPrefix(s string, prefixes ...string) (string, bool) {
	lower := strings.ToLower(s)
	for _, p := range prefixes {
		pl := strings.ToLower(p)
		if strings.HasPrefix(lower, pl) {
			return s[len(p):], true
		}
	}
	return "", false
}
