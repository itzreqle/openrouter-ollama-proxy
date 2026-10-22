package handlers

import (
	"bytes"
	"context"
	"errors"
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
		a.Log.Error("upstream chat failed", "err", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	streamCopy(w, resp)
}

// ListModels handles GET /v1/models (merged OpenAI format, ranked OpenRouter).
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
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	now := time.Now().Unix()
	data := make([]item, 0, 64)

	if models, err := a.Ollama.ListModels(ctx); err == nil {
		for _, m := range models {
			data = append(data, item{
				ID:      "ollama/" + m.Name,
				Object:  "model",
				Created: now,
				OwnedBy: "ollama",
			})
		}
	} else {
		a.Log.Warn("list ollama models failed", "err", err)
	}

	if a.Catalog != nil {
		for _, m := range a.Catalog.Entries() {
			data = append(data, item{
				ID:      m.PrefixedID,
				Object:  "model",
				Created: now,
				OwnedBy: "openrouter",
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}
