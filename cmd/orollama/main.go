package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/catalog"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/config"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/handlers"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/httpserver"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/logx"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/model"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/ollama"
	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/openrouter"
)

func main() {
	log := logx.NewLogger(os.Stdout, slog.LevelInfo)

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	if cfg.OpenRouterAPIKey == "" {
		log.Warn("OPENROUTER_API_KEY is empty; OpenRouter routes will fail until it is set")
	}
	if !isLoopbackHost(cfg.Host) && strings.TrimSpace(cfg.ProxyAuthToken) == "" {
		log.Warn("SECURITY: HOST is non-loopback and PROXY_AUTH_TOKEN is empty — this proxy will accept unauthenticated requests from the network",
			"host", cfg.Host,
			"hint", "set PROXY_AUTH_TOKEN or bind HOST=127.0.0.1",
		)
	}

	orClient := openrouter.New(cfg.OpenRouterBaseURL, cfg.OpenRouterAPIKey, cfg.OpenRouterFreeOnly)
	orClient.Sort = cfg.OpenRouterSort
	orClient.ToolsOnly = cfg.OpenRouterToolsOnly

	cat := catalog.New(cfg, orClient)

	tagCache := model.NewTagCache(time.Duration(cfg.OllamaTagCacheSeconds) * time.Second)
	router := &model.Router{TagCache: tagCache}
	router.SetAliases(model.Aliases{
		Opus:    cfg.ModelOpus,
		Sonnet:  cfg.ModelSonnet,
		Haiku:   cfg.ModelHaiku,
		Default: cfg.Model,
	})

	api := &handlers.API{
		Cfg:        cfg,
		Router:     router,
		Ollama:     ollama.New(cfg.OllamaBaseURL),
		OpenRouter: orClient,
		Catalog:    cat,
		Log:        log,
	}

	ctx, cancelBg := context.WithCancel(context.Background())
	defer cancelBg()

	// Blocking first catalog refresh so banner shows real auto-picked models.
	refreshCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	if err := cat.Refresh(refreshCtx); err != nil {
		log.Warn("initial catalog refresh failed", "err", err)
	} else {
		cat.ApplyToRouter(router)
		log.Info("catalog ready",
			"models", len(cat.Entries()),
			"sort", cfg.OpenRouterSort,
			"free_only", cfg.OpenRouterFreeOnly,
		)
	}
	// Seed Claude Code /model picker (OpenRouter + local Ollama).
	api.SyncClaudePicker(refreshCtx)
	if info, err := orClient.GetKey(refreshCtx); err != nil {
		log.Warn("openrouter key snapshot skipped", "err", err)
	} else if info.FreeModelDailyRequests != nil {
		log.Info("openrouter free daily requests",
			"remaining", info.FreeModelDailyRequests.Remaining,
			"limit", info.FreeModelDailyRequests.Limit,
			"used", info.FreeModelDailyRequests.Used,
		)
	}
	cancel()

	api.StartBackground(ctx)

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           httpserver.New(api, cfg.ProxyAuthToken),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		res := cat.Resolved()
		logx.Banner(os.Stdout,
			"http://"+cfg.Addr(),
			cfg.OllamaBaseURL,
			cfg.OpenRouterBaseURL,
			cfg.ProxyAuthToken != "",
			logx.RoleLine{ID: res.Opus, Auto: res.OpusAuto},
			logx.RoleLine{ID: res.Sonnet, Auto: res.SonnetAuto},
			logx.RoleLine{ID: res.Haiku, Auto: res.HaikuAuto},
		)
		log.Info("ready — waiting for requests",
			"resolved_env", catalog.ResolvedEnvPath(),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	sig := <-sigCh
	log.Info("shutting down", "signal", sig.String())
	cancelBg()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	_ = srv.Shutdown(shutdownCtx)
}

func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return true
	}
	// Strip optional brackets for IPv6 literals.
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	if ip != nil {
		return ip.IsLoopback()
	}
	return false
}
