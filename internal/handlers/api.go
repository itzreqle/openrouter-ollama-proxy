package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/catalog"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/config"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/model"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/ollama"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/openrouter"
)

// API wires handlers to upstreams and the model router.
type API struct {
	Cfg        config.Config
	Router     *model.Router
	Ollama     *ollama.Client
	OpenRouter *openrouter.Client
	Catalog    *catalog.Catalog
	Log        *slog.Logger

	refreshMu sync.Mutex
}

// EnsureTags refreshes the Ollama tag cache when stale.
func (a *API) EnsureTags(ctx context.Context) {
	if a.Router.TagCache == nil || !a.Router.TagCache.Stale() {
		return
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if !a.Router.TagCache.Stale() {
		return
	}
	names, err := a.Ollama.ModelNames(ctx)
	if err != nil {
		a.Log.Warn("ollama tag refresh failed", "err", err)
		return
	}
	a.Router.TagCache.Set(names)
}

// EnsureCatalog refreshes the OpenRouter ranked catalog when stale.
func (a *API) EnsureCatalog(ctx context.Context) {
	if a.Catalog == nil || !a.Catalog.Stale() {
		return
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if !a.Catalog.Stale() {
		return
	}
	if err := a.Catalog.Refresh(ctx); err != nil {
		a.Log.Warn("openrouter catalog refresh failed", "err", err)
		return
	}
	a.Catalog.ApplyToRouter(a.Router)
	a.SyncClaudePicker(ctx)
	a.Log.Info("catalog refreshed",
		"models", len(a.Catalog.Entries()),
		"opus", a.Catalog.Resolved().Opus,
		"sonnet", a.Catalog.Resolved().Sonnet,
		"haiku", a.Catalog.Resolved().Haiku,
	)
}

// SyncClaudePicker writes ~/.claude/settings.json modelPicker from the live catalog.
func (a *API) SyncClaudePicker(ctx context.Context) {
	if a.Catalog == nil {
		return
	}
	extra := make([]catalog.PickerOption, 0, 8)
	if local, err := a.Ollama.ListModels(ctx); err == nil {
		for _, m := range local {
			name := m.Name
			if name == "" {
				continue
			}
			extra = append(extra, catalog.PickerOption{
				Model:       "ollama-claude/" + name,
				Label:       name,
				Description: "Local Ollama model via orollama",
			})
		}
	}
	if err := a.Catalog.WriteClaudeSettings(extra); err != nil {
		a.Log.Warn("claude settings picker write failed", "err", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "invalid_request_error",
		},
	})
}

func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, 32<<20)) // 32 MiB
}

func extractModel(body []byte) (string, map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return "", nil, err
	}
	modelStr, _ := m["model"].(string)
	return modelStr, m, nil
}

func streamCopy(dst http.ResponseWriter, src *http.Response) {
	for k, vv := range src.Header {
		switch http.CanonicalHeaderKey(k) {
		case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
			"Te", "Trailers", "Transfer-Encoding", "Upgrade":
			continue
		}
		for _, v := range vv {
			dst.Header().Add(k, v)
		}
	}
	dst.WriteHeader(src.StatusCode)
	flusher, _ := dst.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Body.Read(buf)
		if n > 0 {
			_, _ = dst.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
}

func (a *API) startTagRefresher(ctx context.Context) {
	ttl := time.Duration(a.Cfg.OllamaTagCacheSeconds) * time.Second
	ticker := time.NewTicker(ttl)
	go func() {
		defer ticker.Stop()
		a.EnsureTags(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.EnsureTags(context.Background())
			}
		}
	}()
}

func (a *API) startCatalogRefresher(ctx context.Context) {
	if a.Catalog == nil {
		return
	}
	ttl := time.Duration(a.Cfg.CatalogCacheSeconds) * time.Second
	ticker := time.NewTicker(ttl)
	go func() {
		defer ticker.Stop()
		a.EnsureCatalog(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.EnsureCatalog(context.Background())
			}
		}
	}()
}

// StartBackground starts periodic Ollama tag + OpenRouter catalog refresh.
func (a *API) StartBackground(ctx context.Context) {
	a.startTagRefresher(ctx)
	a.startCatalogRefresher(ctx)
}
