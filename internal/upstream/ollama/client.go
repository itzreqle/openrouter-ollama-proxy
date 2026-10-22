package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to a local Ollama server.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// New creates an Ollama client.
func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 0, // streaming; per-request contexts apply
		},
	}
}

// Model is a subset of Ollama's /api/tags model object.
type Model struct {
	Name       string `json:"name"`
	Model      string `json:"model,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
	Size       int64  `json:"size,omitempty"`
	Digest     string `json:"digest,omitempty"`
	Details    any    `json:"details,omitempty"`
}

type tagsResponse struct {
	Models []Model `json:"models"`
}

// ListModels fetches /api/tags.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("ollama list models: %s: %s", resp.Status, string(b))
	}
	var out tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

// ModelNames returns just the names from /api/tags.
func (c *Client) ModelNames(ctx context.Context) ([]string, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Name)
	}
	return names, nil
}

// Ping checks that Ollama responds on GET /.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ollama ping: %s", resp.Status)
	}
	return nil
}

// Proxy forwards an HTTP request to the Ollama base URL + path, streaming the response.
func (c *Client) Proxy(ctx context.Context, method, path string, body io.Reader, header http.Header) (*http.Response, error) {
	url := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	copyHopHeaders(req.Header, header)
	return c.HTTPClient.Do(req)
}

func copyHopHeaders(dst, src http.Header) {
	if src == nil {
		return
	}
	// Do not forward Authorization / x-api-key — those are proxy client credentials.
	for _, k := range []string{
		"Content-Type",
		"Accept",
		"anthropic-version",
		"anthropic-beta",
	} {
		if v := src.Get(k); v != "" {
			dst.Set(k, v)
		}
	}
}
