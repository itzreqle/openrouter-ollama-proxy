package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to OpenRouter's OpenAI-compatible API.
type Client struct {
	BaseURL    string
	APIKey     string
	FreeOnly   bool
	HTTPClient *http.Client
	HTTPReferer string
	AppTitle    string
}

// New creates an OpenRouter client.
func New(baseURL, apiKey string, freeOnly bool) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		APIKey:      apiKey,
		FreeOnly:    freeOnly,
		HTTPReferer: "https://github.com/itzreqle/openrouter-ollama-proxy",
		AppTitle:    "orollama",
		HTTPClient: &http.Client{
			Timeout: 0,
		},
	}
}

// Model is a subset of OpenRouter's /models entry.
type Model struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextLength int    `json:"context_length,omitempty"`
}

type modelsResponse struct {
	Data []Model `json:"data"`
}

// ListModels fetches /models (OpenAI-style).
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is not set")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("openrouter list models: %s: %s", resp.Status, string(b))
	}
	var out modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !c.FreeOnly {
		return out.Data, nil
	}
	filtered := make([]Model, 0, len(out.Data))
	for _, m := range out.Data {
		if strings.Contains(m.ID, ":free") {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

// Ping verifies the API key can list models.
func (c *Client) Ping(ctx context.Context) error {
	if c.APIKey == "" {
		return fmt.Errorf("OPENROUTER_API_KEY is not set")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	_, err := c.ListModels(ctx)
	return err
}

// Proxy forwards to OpenRouter path with auth injected.
func (c *Client) Proxy(ctx context.Context, method, path string, body io.Reader, header http.Header) (*http.Response, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is not set")
	}
	url := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if header != nil {
		if ct := header.Get("Content-Type"); ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if a := header.Get("Accept"); a != "" {
			req.Header.Set("Accept", a)
		}
		if v := header.Get("anthropic-version"); v != "" {
			req.Header.Set("anthropic-version", v)
		}
		if v := header.Get("anthropic-beta"); v != "" {
			req.Header.Set("anthropic-beta", v)
		}
	}
	if req.Header.Get("Content-Type") == "" && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuth(req)
	return c.HTTPClient.Do(req)
}

func (c *Client) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("HTTP-Referer", c.HTTPReferer)
	req.Header.Set("X-Title", c.AppTitle)
}
