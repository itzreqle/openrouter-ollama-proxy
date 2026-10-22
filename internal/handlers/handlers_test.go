package handlers_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/config"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/handlers"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/httpserver"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/model"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/ollama"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/openrouter"
)

func testAPI(t *testing.T, ollamaURL, orURL string) *handlers.API {
	t.Helper()
	cache := model.NewTagCache(time.Minute)
	// Seed so EnsureTags does not race the test upstream with a background refresh.
	cache.Set([]string{"dummy"})
	return &handlers.API{
		Cfg: config.Config{
			OllamaBaseURL:     ollamaURL,
			OpenRouterBaseURL: orURL,
			OpenRouterAPIKey:  "test-key",
		},
		Router: func() *model.Router {
			r := &model.Router{TagCache: cache}
			r.SetAliases(model.Aliases{Sonnet: "openrouter/anthropic/claude-sonnet-4"})
			return r
		}(),
		Ollama:     ollama.New(ollamaURL),
		OpenRouter: openrouter.New(orURL, "test-key", false),
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestRootAndAuth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	api := testAPI(t, upstream.URL, upstream.URL)
	h := httpserver.New(api, "secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != "Ollama is running" {
		t.Fatalf("root: code=%d body=%q", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with auth, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMessagesRoutesToOpenRouter(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	var gotAuth string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"anthropic/claude-sonnet-4","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(upstream.Close)

	api := testAPI(t, upstream.URL, upstream.URL)
	h := httpserver.New(api, "")

	body := `{"model":"openrouter/anthropic/claude-sonnet-4","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if gotPath != "/messages" {
		t.Fatalf("path=%q want /messages", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotBody["model"] != "anthropic/claude-sonnet-4" {
		t.Fatalf("upstream model=%v", gotBody["model"])
	}
}

func TestMessagesAliasAndOllamaPath(t *testing.T) {
	var gotPath string
	var gotModel string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		gotModel, _ = m["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	api := testAPI(t, upstream.URL, upstream.URL)
	api.Router.SetAliases(model.Aliases{Sonnet: "ollama/qwen3-coder"})
	api.Router.TagCache.Set([]string{"qwen3-coder"})

	h := httpserver.New(api, "")
	body := `{"model":"claude-3-5-sonnet-latest","max_tokens":8,"messages":[{"role":"user","content":"x"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("path=%q", gotPath)
	}
	if gotModel != "qwen3-coder" {
		t.Fatalf("model=%q", gotModel)
	}
}

func TestUnknownModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	api := testAPI(t, upstream.URL, upstream.URL)
	h := httpserver.New(api, "")

	body := `{"model":"nope","max_tokens":8,"messages":[{"role":"user","content":"x"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rr.Code)
	}
}
