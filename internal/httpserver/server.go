package httpserver

import (
	"net/http"
	"strings"
	"time"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/handlers"
)

// New builds the HTTP handler mux with optional auth middleware.
func New(api *handlers.API, proxyAuthToken string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", api.Root)
	mux.HandleFunc("/healthz", api.Healthz)
	mux.HandleFunc("/orollama/catalog", api.CatalogDebug)

	mux.HandleFunc("/v1/messages", api.Messages)
	mux.HandleFunc("/v1/chat/completions", api.ChatCompletions)
	mux.HandleFunc("/v1/models", api.ListModels)

	mux.HandleFunc("/api/tags", api.Tags)
	mux.HandleFunc("/api/show", api.Show)
	mux.HandleFunc("/api/chat", api.Chat)

	var h http.Handler = mux
	h = withLogging(api, h)
	if proxyAuthToken != "" {
		h = withAuth(proxyAuthToken, h)
	}
	return h
}

func withAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// health endpoints stay open for local probes
		if r.URL.Path == "/" || r.URL.Path == "/healthz" || r.URL.Path == "/orollama/catalog" {
			next.ServeHTTP(w, r)
			return
		}
		if !authorized(r, token) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"unauthorized","type":"authentication_error"}}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authorized(r *http.Request, token string) bool {
	if ah := r.Header.Get("Authorization"); ah != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(ah, prefix) && strings.TrimSpace(ah[len(prefix):]) == token {
			return true
		}
		if ah == token {
			return true
		}
	}
	if x := r.Header.Get("x-api-key"); x != "" && x == token {
		return true
	}
	return false
}

func withLogging(api *handlers.API, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if api.Log != nil && r.URL.Path != "/healthz" {
			api.Log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher when the underlying writer supports it.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
