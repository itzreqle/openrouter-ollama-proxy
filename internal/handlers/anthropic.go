package handlers

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/model"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/translate"
)

// Messages handles POST /v1/messages (Anthropic Messages API).
func (a *API) Messages(w http.ResponseWriter, r *http.Request) {
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

	a.Log.Info("anthropic messages",
		"original", route.Original,
		"backend", route.Backend,
		"upstream", route.Upstream,
	)

	var resp *http.Response
	switch route.Backend {
	case model.BackendOllama:
		resp, err = a.Ollama.Proxy(r.Context(), http.MethodPost, "/v1/messages", bytes.NewReader(rewritten), r.Header)
	case model.BackendOpenRouter:
		// OpenRouter Anthropic skin lives at /api/v1/messages; our base already includes /api/v1
		resp, err = a.OpenRouter.Proxy(r.Context(), http.MethodPost, "/messages", bytes.NewReader(rewritten), r.Header)
	default:
		writeError(w, http.StatusInternalServerError, "unknown backend")
		return
	}
	if err != nil {
		logUpstreamTransportError(a.Log, "messages", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	if route.Backend == model.BackendOpenRouter {
		maybeLogOpenRouterError(a.Log, route.Upstream, resp)
	}
	streamCopy(w, resp)
}
