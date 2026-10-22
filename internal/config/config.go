package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds runtime settings loaded from the environment.
type Config struct {
	Host string
	Port int

	OpenRouterAPIKey   string
	OpenRouterBaseURL  string
	OpenRouterFreeOnly bool
	OpenRouterSort     string
	OpenRouterToolsOnly bool

	OllamaBaseURL         string
	OllamaTagCacheSeconds int
	CatalogCacheSeconds   int

	ProxyAuthToken string

	ModelOpus   string
	ModelSonnet string
	ModelHaiku  string
	Model       string
}

// Addr returns host:port.
func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Load reads optional .env then environment variables.
func Load() (Config, error) {
	// Overload so empty MODEL_* in .env clears stale shell exports (auto-catalog).
	_ = godotenv.Overload() // ignore missing .env

	cfg := Config{
		Host:                envOr("HOST", "127.0.0.1"),
		Port:                envInt("PORT", 8082),
		OpenRouterAPIKey:    os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterBaseURL:   strings.TrimRight(envOr("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"), "/"),
		OpenRouterFreeOnly:  envBool("OPENROUTER_FREE_ONLY", true),
		OpenRouterSort:      envOr("OPENROUTER_SORT", "intelligence-high-to-low"),
		OpenRouterToolsOnly: envBool("OPENROUTER_TOOLS_ONLY", true),
		OllamaBaseURL:       strings.TrimRight(envOr("OLLAMA_BASE_URL", "http://127.0.0.1:11434"), "/"),
		OllamaTagCacheSeconds: envInt("OLLAMA_TAG_CACHE_SECONDS", 30),
		CatalogCacheSeconds:   envInt("CATALOG_CACHE_SECONDS", 600),
		ProxyAuthToken:        os.Getenv("PROXY_AUTH_TOKEN"),
		ModelOpus:             strings.TrimSpace(os.Getenv("MODEL_OPUS")),
		ModelSonnet:           strings.TrimSpace(os.Getenv("MODEL_SONNET")),
		ModelHaiku:            strings.TrimSpace(os.Getenv("MODEL_HAIKU")),
		Model:                 strings.TrimSpace(os.Getenv("MODEL")),
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return Config{}, fmt.Errorf("invalid PORT: %d", cfg.Port)
	}
	if cfg.OllamaTagCacheSeconds < 1 {
		cfg.OllamaTagCacheSeconds = 30
	}
	if cfg.CatalogCacheSeconds < 30 {
		cfg.CatalogCacheSeconds = 600
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
