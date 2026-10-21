package handlers

import (
	"context"
	"net/http"
	"time"
)

// Root returns Ollama-compatible health text.
func (a *API) Root(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte("Ollama is running"))
	}
}

// Healthz reports upstream reachability.
func (a *API) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	ollamaOK := true
	ollamaErr := ""
	if err := a.Ollama.Ping(ctx); err != nil {
		ollamaOK = false
		ollamaErr = err.Error()
	}

	orOK := true
	orErr := ""
	if a.Cfg.OpenRouterAPIKey == "" {
		orOK = false
		orErr = "OPENROUTER_API_KEY is not set"
	} else if err := a.OpenRouter.Ping(ctx); err != nil {
		orOK = false
		orErr = err.Error()
	}

	status := http.StatusOK
	if !ollamaOK && !orOK {
		status = http.StatusServiceUnavailable
	}

	writeJSON(w, status, map[string]any{
		"ok": ollamaOK || orOK,
		"ollama": map[string]any{
			"ok":    ollamaOK,
			"error": ollamaErr,
			"url":   a.Cfg.OllamaBaseURL,
		},
		"openrouter": map[string]any{
			"ok":    orOK,
			"error": orErr,
			"url":   a.Cfg.OpenRouterBaseURL,
		},
	})
}
