package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/catalog"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/claudesettings"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/config"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/model"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/ollama"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/openrouter"
	"golang.org/x/sync/singleflight"
)

// API wires handlers to upstreams and the model router.
type API struct {
	Cfg        config.Config
	Router     *model.Router
	Ollama     *ollama.Client
	OpenRouter *openrouter.Client
	Catalog    *catalog.Catalog
	Log        *slog.Logger

	catFlight singleflight.Group
	tagFlight singleflight.Group
}

// EnsureTags returns immediately; when the tag cache is stale it kicks a
// background singleflight refresh so request paths never block on Ollama.
func (a *API) EnsureTags(ctx context.Context) {
	_ = ctx
	if a.Router.TagCache == nil || !a.Router.TagCache.Stale() {
		return
	}
	go a.refreshTagsBackground()
}

func (a *API) refreshTagsBackground() {
	_, _, _ = a.tagFlight.Do("tags", func() (any, error) {
		if a.Router.TagCache == nil || !a.Router.TagCache.Stale() {
			return nil, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		names, err := a.Ollama.ModelNames(ctx)
		if err != nil {
			a.Log.Warn("ollama tag refresh failed", "err", err)
			return nil, err
		}
		a.Router.TagCache.Set(names)
		return nil, nil
	})
}

// EnsureCatalog returns immediately with the current (possibly stale) catalog.
// When TTL expires it triggers a background singleflight refresh.
func (a *API) EnsureCatalog(ctx context.Context) {
	_ = ctx
	if a.Catalog == nil || !a.Catalog.Stale() {
		return
	}
	go a.refreshCatalogBackground()
}

func (a *API) refreshCatalogBackground() {
	_, _, _ = a.catFlight.Do("catalog", func() (any, error) {
		if a.Catalog == nil || !a.Catalog.Stale() {
			return nil, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := a.Catalog.Refresh(ctx); err != nil {
			a.Log.Warn("openrouter catalog refresh failed", "err", err)
			return nil, err
		}
		a.Catalog.ApplyToRouter(a.Router)
		a.SyncClaudePicker(ctx)
		a.Log.Info("catalog refreshed",
			"models", len(a.Catalog.Entries()),
			"opus", a.Catalog.Resolved().Opus,
			"sonnet", a.Catalog.Resolved().Sonnet,
			"haiku", a.Catalog.Resolved().Haiku,
		)
		return nil, nil
	})
}

// SyncClaudePicker writes ~/.claude/settings.json modelPicker from the live catalog.
// Role env vars (ANTHROPIC_DEFAULT_*) are owned by the shell hook, not this writer.
func (a *API) SyncClaudePicker(ctx context.Context) {
	if a.Catalog == nil {
		return
	}
	if !claudesettings.Enabled() {
		return
	}
	opts := make([]claudesettings.Option, 0, len(a.Catalog.Entries())+8)
	seen := map[string]struct{}{}
	add := func(o claudesettings.Option) {
		if o.Model == "" {
			return
		}
		if _, ok := seen[o.Model]; ok {
			return
		}
		seen[o.Model] = struct{}{}
		opts = append(opts, o)
	}

	res := a.Catalog.Resolved()
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

	for _, e := range a.Catalog.Entries() {
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
		add(claudesettings.Option{Model: e.PrefixedID, Label: label, Description: desc})
	}

	if local, err := a.Ollama.ListModels(ctx); err == nil {
		for _, m := range local {
			name := m.Name
			if name == "" {
				continue
			}
			add(claudesettings.Option{
				Model:       "ollama-claude/" + name,
				Label:       name,
				Description: "Local Ollama model via orollama",
			})
		}
	}

	if err := claudesettings.WriteModelPicker(opts); err != nil {
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
