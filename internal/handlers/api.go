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
		// skip hop-by-hop
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

// StartBackground starts periodic Ollama tag refresh.
func (a *API) StartBackground(ctx context.Context) {
	a.startTagRefresher(ctx)
}
