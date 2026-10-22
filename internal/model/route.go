package model

import (
	"strings"
	"sync"
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
	Backend   Backend
	Upstream  string // model id sent to the upstream
	Original  string
}

// Aliases maps Claude Code role names to configured model ids.
type Aliases struct {
	Opus   string
	Sonnet string
	Haiku  string
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
	Aliases  Aliases
	TagCache *TagCache
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

	// Never rewrite explicitly routed ids.
	if strings.HasPrefix(lower, "ollama-claude/") || strings.HasPrefix(lower, "ollama-claude:") ||
		strings.HasPrefix(lower, "openrouter-claude/") || strings.HasPrefix(lower, "openrouter-claude:") ||
		strings.HasPrefix(lower, "ollama/") || strings.HasPrefix(lower, "ollama:") ||
		strings.HasPrefix(lower, "openrouter/") || strings.HasPrefix(lower, "openrouter:") {
		return ""
	}

	switch {
	case strings.Contains(lower, "opus"):
		if r.Aliases.Opus != "" {
			return r.Aliases.Opus
		}
	case strings.Contains(lower, "sonnet"):
		if r.Aliases.Sonnet != "" {
			return r.Aliases.Sonnet
		}
	case strings.Contains(lower, "haiku"):
		if r.Aliases.Haiku != "" {
			return r.Aliases.Haiku
		}
	case strings.HasPrefix(lower, "claude-"):
		if r.Aliases.Default != "" {
			return r.Aliases.Default
		}
	}

	if r.Aliases.Default != "" && (strings.HasPrefix(lower, "claude") || lower == "default") {
		return r.Aliases.Default
	}
	return ""
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
