package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/model"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/translate"
)

// ChatCompletions handles POST /v1/chat/completions.
func (a *API) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	a.EnsureTags(r.Context())
	a.EnsureCatalog(r.Context())

	body, err := readBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	modelName, _, err := extractModel(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	route, err := a.Router.Resolve(modelName)
	if err != nil {
		var unk *model.UnknownModelError
		if errors.As(err, &unk) || errors.Is(err, model.ErrEmptyModel) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rewritten, err := translate.RewriteJSONModel(body, route.Upstream)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	a.Log.Info("openai chat",
		"original", route.Original,
		"backend", route.Backend,
		"upstream", route.Upstream,
	)

	var resp *http.Response
	switch route.Backend {
	case model.BackendOllama:
		resp, err = a.Ollama.Proxy(r.Context(), http.MethodPost, "/v1/chat/completions", bytes.NewReader(rewritten), r.Header)
	case model.BackendOpenRouter:
		resp, err = a.OpenRouter.Proxy(r.Context(), http.MethodPost, "/chat/completions", bytes.NewReader(rewritten), r.Header)
	default:
		writeError(w, http.StatusInternalServerError, "unknown backend")
		return
	}
	if err != nil {
		logUpstreamTransportError(a.Log, "chat", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	if route.Backend == model.BackendOpenRouter {
		maybeLogOpenRouterError(a.Log, route.Upstream, resp)
	}
	streamCopy(w, resp)
}

// ListModels handles GET /v1/models (merged OpenAI + Anthropic discovery fields).
// Ids use *-claude/ prefixes so Claude Code gateway discovery keeps them
// (it filters to ids containing "claude" or "anthropic").
func (a *API) ListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	a.EnsureTags(ctx)
	a.EnsureCatalog(ctx)

	type item struct {
		ID          string `json:"id"`
		Object      string `json:"object"`
		Created     int64  `json:"created"`
		OwnedBy     string `json:"owned_by"`
		DisplayName string `json:"display_name,omitempty"`
		Description string `json:"description,omitempty"`
	}

	now := time.Now().Unix()
	data := make([]item, 0, 64)

	if models, err := a.Ollama.ListModels(ctx); err == nil {
		for _, m := range models {
			id := "ollama-claude/" + m.Name
			data = append(data, item{
				ID:          id,
				Object:      "model",
				Created:     now,
				OwnedBy:     "ollama",
				DisplayName: m.Name,
				Description: "Local Ollama model via orollama",
			})
		}
	} else {
		a.Log.Warn("list ollama models failed", "err", err)
	}

	if a.Catalog != nil {
		for _, m := range a.Catalog.Entries() {
			desc := "OpenRouter via orollama"
			if m.Free {
				desc = "OpenRouter :free via orollama"
			}
			if m.ContextLength > 0 {
				desc = fmt.Sprintf("%s · %dk context", desc, m.ContextLength/1000)
			}
			name := m.Name
			if name == "" {
				name = m.ID
			}
			data = append(data, item{
				ID:          m.PrefixedID,
				Object:      "model",
				Created:     now,
				OwnedBy:     "openrouter",
				DisplayName: name,
				Description: desc,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}
