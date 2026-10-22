package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to OpenRouter's OpenAI-compatible API.
type Client struct {
	BaseURL     string
	APIKey      string
	FreeOnly    bool
	Sort        string
	ToolsOnly   bool
	HTTPClient  *http.Client
	HTTPReferer string
	AppTitle    string
}

// New creates an OpenRouter client.
func New(baseURL, apiKey string, freeOnly bool) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		APIKey:      apiKey,
		FreeOnly:    freeOnly,
		Sort:        "intelligence-high-to-low",
		ToolsOnly:   true,
		HTTPReferer: "https://github.com/itzreqle/openrouter-ollama-proxy",
		AppTitle:    "orollama",
		HTTPClient: &http.Client{
			Timeout: 0,
		},
	}
}

// Model is a subset of OpenRouter's /models entry.
type Model struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name,omitempty"`
	ContextLength      int      `json:"context_length,omitempty"`
	SupportedParameters []string `json:"supported_parameters,omitempty"`
	Pricing            *Pricing `json:"pricing,omitempty"`
}

// Pricing holds OpenRouter pricing strings (USD per token).
type Pricing struct {
	Prompt     string `json:"prompt,omitempty"`
	Completion string `json:"completion,omitempty"`
}

type modelsResponse struct {
	Data []Model `json:"data"`
}

// ListOptions controls catalog queries.
type ListOptions struct {
	Sort      string
	ToolsOnly bool
	FreeOnly  bool
}

// ListModels fetches /models with optional ranking filters.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	return c.ListModelsRanked(ctx, ListOptions{
		Sort:      c.Sort,
		ToolsOnly: c.ToolsOnly,
		FreeOnly:  c.FreeOnly,
	})
}

// ListModelsRanked fetches models using OpenRouter server-side sort/filters.
func (c *Client) ListModelsRanked(ctx context.Context, opts ListOptions) ([]Model, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is not set")
	}

	sort := opts.Sort
	if sort == "" {
		sort = "intelligence-high-to-low"
	}

	q := url.Values{}
	q.Set("sort", sort)
	q.Set("output_modalities", "text")
	if opts.ToolsOnly {
		q.Set("supported_parameters", "tools")
	}

	var all []Model
	offset := 0
	limit := 500
	for {
		q.Set("offset", strconv.Itoa(offset))
		q.Set("limit", strconv.Itoa(limit))
		page, nextOffset, err := c.fetchModelsPage(ctx, q)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if nextOffset < 0 || len(page) == 0 {
			break
		}
		offset = nextOffset
		if offset > 5000 {
			break
		}
	}

	if !opts.FreeOnly {
		return all, nil
	}
	filtered := make([]Model, 0, len(all))
	for _, m := range all {
		if isFreeModel(m) {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

func isFreeModel(m Model) bool {
	if strings.Contains(m.ID, ":free") {
		return true
	}
	if m.Pricing == nil {
		return false
	}
	return isZeroPrice(m.Pricing.Prompt) && isZeroPrice(m.Pricing.Completion)
}

func isZeroPrice(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s == "0"
	}
	return f == 0
}

func (c *Client) fetchModelsPage(ctx context.Context, q url.Values) ([]Model, int, error) {
	u := c.BaseURL + "/models?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, -1, err
	}
	c.setAuth(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, -1, fmt.Errorf("openrouter list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, -1, fmt.Errorf("openrouter list models: %s: %s", resp.Status, string(b))
	}
	var out modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, -1, err
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 {
		limit = 500
	}
	if len(out.Data) < limit {
		return out.Data, -1, nil
	}
	return out.Data, offset + len(out.Data), nil
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
