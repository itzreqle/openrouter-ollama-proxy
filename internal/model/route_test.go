package model

import (
	"errors"
	"testing"
	"time"
)

func TestResolvePrefixes(t *testing.T) {
	r := &Router{TagCache: NewTagCache(time.Minute)}

	cases := []struct {
		in       string
		backend  Backend
		upstream string
	}{
		{"ollama/qwen3-coder", BackendOllama, "qwen3-coder"},
		{"ollama:llama3", BackendOllama, "llama3"},
		{"openrouter/anthropic/claude-sonnet-4", BackendOpenRouter, "anthropic/claude-sonnet-4"},
		{"openrouter:deepseek/deepseek-r1", BackendOpenRouter, "deepseek/deepseek-r1"},
		{"deepseek/deepseek-r1:free", BackendOpenRouter, "deepseek/deepseek-r1:free"},
	}

	for _, tc := range cases {
		got, err := r.Resolve(tc.in)
		if err != nil {
			t.Fatalf("%s: unexpected err: %v", tc.in, err)
		}
		if got.Backend != tc.backend || got.Upstream != tc.upstream {
			t.Fatalf("%s: got %+v want backend=%s upstream=%s", tc.in, got, tc.backend, tc.upstream)
		}
	}
}

func TestResolveLocalTag(t *testing.T) {
	cache := NewTagCache(time.Minute)
	cache.Set([]string{"qwen3-coder:latest"})
	r := &Router{TagCache: cache}

	got, err := r.Resolve("qwen3-coder")
	if err != nil {
		t.Fatal(err)
	}
	if got.Backend != BackendOllama || got.Upstream != "qwen3-coder" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveUnknown(t *testing.T) {
	r := &Router{TagCache: NewTagCache(time.Minute)}
	_, err := r.Resolve("totally-missing-model")
	var unk *UnknownModelError
	if !errors.As(err, &unk) {
		t.Fatalf("expected UnknownModelError, got %v", err)
	}
}

func TestResolveAliases(t *testing.T) {
	r := &Router{
		Aliases: Aliases{
			Opus:    "openrouter/anthropic/claude-opus-4",
			Sonnet:  "ollama/qwen3-coder",
			Haiku:   "ollama/tinyllama",
			Default: "openrouter/anthropic/claude-sonnet-4",
		},
		TagCache: NewTagCache(time.Minute),
	}
	r.TagCache.Set([]string{"qwen3-coder", "tinyllama"})

	cases := []struct {
		in       string
		backend  Backend
		upstream string
	}{
		{"claude-opus-4-20250514", BackendOpenRouter, "anthropic/claude-opus-4"},
		{"claude-3-5-sonnet-latest", BackendOllama, "qwen3-coder"},
		{"claude-3-haiku-20240307", BackendOllama, "tinyllama"},
	}

	for _, tc := range cases {
		got, err := r.Resolve(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got.Backend != tc.backend || got.Upstream != tc.upstream {
			t.Fatalf("%s: got %+v want backend=%s upstream=%s", tc.in, got, tc.backend, tc.upstream)
		}
	}
}

func TestEmptyModel(t *testing.T) {
	r := &Router{}
	_, err := r.Resolve("  ")
	if !errors.Is(err, ErrEmptyModel) {
		t.Fatalf("got %v", err)
	}
}

func TestExplicitPrefixSkipsAlias(t *testing.T) {
	r := &Router{
		Aliases: Aliases{
			Sonnet: "ollama/other",
		},
		TagCache: NewTagCache(time.Minute),
	}
	got, err := r.Resolve("openrouter/anthropic/claude-sonnet-4")
	if err != nil {
		t.Fatal(err)
	}
	if got.Backend != BackendOpenRouter || got.Upstream != "anthropic/claude-sonnet-4" {
		t.Fatalf("got %+v", got)
	}
}
