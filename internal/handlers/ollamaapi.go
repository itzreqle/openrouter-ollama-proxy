package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/model"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/translate"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/ollama"
)

// Tags handles GET /api/tags — merge local Ollama + OpenRouter catalogs.
func (a *API) Tags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	a.EnsureCatalog(ctx)

	models := make([]ollama.Model, 0, 64)

	if local, err := a.Ollama.ListModels(ctx); err == nil {
		a.Router.TagCache.Set(namesFromLocal(local))
		for _, m := range local {
			prefixed := m
			prefixed.Name = "ollama-claude/" + m.Name
			if m.Model == "" {
				prefixed.Model = prefixed.Name
			} else {
				prefixed.Model = "ollama-claude/" + m.Model
			}
			models = append(models, prefixed)
		}
	} else {
		a.Log.Warn("ollama /api/tags failed", "err", err)
	}

	if a.Catalog != nil {
		now := time.Now().UTC().Format(time.RFC3339)
		for _, m := range a.Catalog.Entries() {
			models = append(models, ollama.Model{
				Name:       m.PrefixedID,
				Model:      m.PrefixedID,
				ModifiedAt: now,
				Size:       0,
				Digest:     "openrouter",
				Details: map[string]any{
					"family":             "openrouter",
					"parameter_size":     "",
					"quantization_level": "",
					"context_length":     m.ContextLength,
					"display_name":       m.Name,
					"free":               m.Free,
				},
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func namesFromLocal(local []ollama.Model) []string {
	names := make([]string, 0, len(local))
	for _, m := range local {
		names = append(names, m.Name)
	}
	return names
}

// Show handles POST /api/show.
func (a *API) Show(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := req.Name
	if name == "" {
		name = req.Model
	}

	route, err := a.Router.Resolve(name)
	if err != nil {
		var unk *model.UnknownModelError
		if errors.As(err, &unk) || errors.Is(err, model.ErrEmptyModel) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch route.Backend {
	case model.BackendOllama:
		payload, _ := json.Marshal(map[string]string{"name": route.Upstream})
		resp, err := a.Ollama.Proxy(r.Context(), http.MethodPost, "/api/show", bytes.NewReader(payload), http.Header{
			"Content-Type": []string{"application/json"},
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		defer resp.Body.Close()
		streamCopy(w, resp)
	case model.BackendOpenRouter:
		writeJSON(w, http.StatusOK, map[string]any{
			"modelfile":  "# openrouter remote model\n",
			"parameters":  "",
			"template":   "",
			"details": map[string]any{
				"family":             "openrouter",
				"parameter_size":     "",
				"quantization_level": "",
			},
			"model_info": map[string]any{
				"general.basename": route.Upstream,
			},
			"capabilities": []string{"completion"},
		})
	default:
		writeError(w, http.StatusInternalServerError, "unknown backend")
	}
}

// Chat handles POST /api/chat.
func (a *API) Chat(w http.ResponseWriter, r *http.Request) {
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

	var req translate.OllamaChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	route, err := a.Router.Resolve(req.Model)
	if err != nil {
		var unk *model.UnknownModelError
		if errors.As(err, &unk) || errors.Is(err, model.ErrEmptyModel) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	a.Log.Info("ollama chat",
		"original", route.Original,
		"backend", route.Backend,
		"upstream", route.Upstream,
	)

	switch route.Backend {
	case model.BackendOllama:
		rewritten, err := translate.RewriteJSONModel(body, route.Upstream)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		resp, err := a.Ollama.Proxy(r.Context(), http.MethodPost, "/api/chat", bytes.NewReader(rewritten), r.Header)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		defer resp.Body.Close()
		streamCopy(w, resp)

	case model.BackendOpenRouter:
		openaiReq, stream := translate.ToOpenAI(req, route.Upstream)
		payload, err := json.Marshal(openaiReq)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp, err := a.OpenRouter.Proxy(r.Context(), http.MethodPost, "/chat/completions", bytes.NewReader(payload), http.Header{
			"Content-Type": []string{"application/json"},
			"Accept":       []string{"text/event-stream"},
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			writeError(w, resp.StatusCode, string(b))
			return
		}

		displayModel := route.Original
		if displayModel == "" {
			displayModel = "openrouter-claude/" + route.Upstream
		}

		if !stream {
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			out, err := translate.FromOpenAIJSON(b, displayModel)
			if err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(out)
			return
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		if err := translate.StreamOpenAIToOllama(resp.Body, w, displayModel); err != nil {
			a.Log.Warn("stream translate ended", "err", err)
		}

	default:
		writeError(w, http.StatusInternalServerError, "unknown backend")
	}
}
