package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/config"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/handlers"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/httpserver"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/model"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/ollama"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/openrouter"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	if cfg.OpenRouterAPIKey == "" {
		log.Warn("OPENROUTER_API_KEY is empty; OpenRouter routes will fail until it is set")
	}

	tagCache := model.NewTagCache(time.Duration(cfg.OllamaTagCacheSeconds) * time.Second)
	router := &model.Router{
		Aliases: model.Aliases{
			Opus:    cfg.ModelOpus,
			Sonnet:  cfg.ModelSonnet,
			Haiku:   cfg.ModelHaiku,
			Default: cfg.Model,
		},
		TagCache: tagCache,
	}

	api := &handlers.API{
		Cfg:    cfg,
		Router: router,
		Ollama: ollama.New(cfg.OllamaBaseURL),
		OpenRouter: openrouter.New(cfg.OpenRouterBaseURL, cfg.OpenRouterAPIKey, cfg.OpenRouterFreeOnly),
		Log:    log,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	api.StartBackground(ctx)

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           httpserver.New(api, cfg.ProxyAuthToken),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("orollama listening",
			"addr", "http://"+cfg.Addr(),
			"ollama", cfg.OllamaBaseURL,
			"openrouter", cfg.OpenRouterBaseURL,
			"auth", cfg.ProxyAuthToken != "",
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
