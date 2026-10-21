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

	OpenRouterAPIKey  string
	OpenRouterBaseURL string
	OpenRouterFreeOnly bool

	OllamaBaseURL          string
	OllamaTagCacheSeconds  int

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
	_ = godotenv.Load() // ignore missing .env

	cfg := Config{
		Host:                   envOr("HOST", "127.0.0.1"),
		Port:                   envInt("PORT", 8082),
		OpenRouterAPIKey:       os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterBaseURL:      strings.TrimRight(envOr("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"), "/"),
		OpenRouterFreeOnly:     envBool("OPENROUTER_FREE_ONLY", false),
		OllamaBaseURL:          strings.TrimRight(envOr("OLLAMA_BASE_URL", "http://127.0.0.1:11434"), "/"),
		OllamaTagCacheSeconds:  envInt("OLLAMA_TAG_CACHE_SECONDS", 30),
		ProxyAuthToken:         os.Getenv("PROXY_AUTH_TOKEN"),
		ModelOpus:              os.Getenv("MODEL_OPUS"),
		ModelSonnet:            os.Getenv("MODEL_SONNET"),
		ModelHaiku:             os.Getenv("MODEL_HAIKU"),
		Model:                  os.Getenv("MODEL"),
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return Config{}, fmt.Errorf("invalid PORT: %d", cfg.Port)
	}
	if cfg.OllamaTagCacheSeconds < 1 {
		cfg.OllamaTagCacheSeconds = 30
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
