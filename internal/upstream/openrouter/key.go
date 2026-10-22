package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// FreeModelDailyRequests is the free-tier daily request counter.
type FreeModelDailyRequests struct {
	Used      int `json:"used"`
	Limit     int `json:"limit"`
	Remaining int `json:"remaining"`
}

// Budget is one spend cap enforced for the key.
type Budget struct {
	Scope         string  `json:"scope"`
	LimitUSD      float64 `json:"limit_usd"`
	SpendUSD      float64 `json:"spend_usd"`
	RemainingUSD  float64 `json:"remaining_usd"`
	ResetInterval string  `json:"reset_interval,omitempty"`
	ResetsAt      string  `json:"resets_at,omitempty"`
}

// KeyInfo is a trimmed view of GET /api/v1/key.
type KeyInfo struct {
	Label                   string                  `json:"label,omitempty"`
	IsFreeTier              bool                    `json:"is_free_tier"`
	Limit                   *float64                `json:"limit"`
	LimitRemaining          *float64                `json:"limit_remaining"`
	Usage                   float64                 `json:"usage"`
	UsageDaily              float64                 `json:"usage_daily"`
	UsageWeekly             float64                 `json:"usage_weekly"`
	UsageMonthly            float64                 `json:"usage_monthly"`
	FreeModelDailyRequests  *FreeModelDailyRequests `json:"free_model_daily_requests,omitempty"`
	EffectiveBudget         *Budget                 `json:"effective_budget,omitempty"`
	FetchedAt               time.Time               `json:"fetched_at"`
}

type keyAPIResponse struct {
	Data struct {
		Label                  string   `json:"label"`
		Limit                  *float64 `json:"limit"`
		LimitRemaining         *float64 `json:"limit_remaining"`
		Usage                  float64  `json:"usage"`
		UsageDaily             float64  `json:"usage_daily"`
		UsageWeekly            float64  `json:"usage_weekly"`
		UsageMonthly           float64  `json:"usage_monthly"`
		IsFreeTier             bool     `json:"is_free_tier"`
		FreeModelDailyRequests *struct {
			Used      int `json:"used"`
			Limit     int `json:"limit"`
			Remaining int `json:"remaining"`
		} `json:"free_model_daily_requests"`
		EffectiveBudget *struct {
			Scope         string  `json:"scope"`
			LimitUSD      float64 `json:"limit_usd"`
			SpendUSD      float64 `json:"spend_usd"`
			RemainingUSD  float64 `json:"remaining_usd"`
			ResetInterval string  `json:"reset_interval"`
			ResetsAt      *string `json:"resets_at"`
		} `json:"effective_budget"`
	} `json:"data"`
}

var (
	keyMu        sync.Mutex
	keyCache     *KeyInfo
	keyCacheUntil time.Time
)

const keyCacheTTL = 60 * time.Second

// GetKey fetches (and caches) the current API key limits snapshot.
func (c *Client) GetKey(ctx context.Context) (*KeyInfo, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is not set")
	}

	keyMu.Lock()
	if keyCache != nil && time.Now().Before(keyCacheUntil) {
		cp := *keyCache
		keyMu.Unlock()
		return &cp, nil
	}
	keyMu.Unlock()

	info, err := c.fetchKey(ctx)
	if err != nil {
		return nil, err
	}

	keyMu.Lock()
	keyCache = info
	keyCacheUntil = time.Now().Add(keyCacheTTL)
	cp := *info
	keyMu.Unlock()
	return &cp, nil
}

func (c *Client) fetchKey(ctx context.Context) (*KeyInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/key", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("openrouter key: %s: %s", resp.Status, string(b))
	}

	var raw keyAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	info := &KeyInfo{
		Label:          raw.Data.Label,
		IsFreeTier:     raw.Data.IsFreeTier,
		Limit:          raw.Data.Limit,
		LimitRemaining: raw.Data.LimitRemaining,
		Usage:          raw.Data.Usage,
		UsageDaily:     raw.Data.UsageDaily,
		UsageWeekly:    raw.Data.UsageWeekly,
		UsageMonthly:   raw.Data.UsageMonthly,
		FetchedAt:      time.Now().UTC(),
	}
	if raw.Data.FreeModelDailyRequests != nil {
		info.FreeModelDailyRequests = &FreeModelDailyRequests{
			Used:      raw.Data.FreeModelDailyRequests.Used,
			Limit:     raw.Data.FreeModelDailyRequests.Limit,
			Remaining: raw.Data.FreeModelDailyRequests.Remaining,
		}
	}
	if raw.Data.EffectiveBudget != nil {
		b := &Budget{
			Scope:         raw.Data.EffectiveBudget.Scope,
			LimitUSD:      raw.Data.EffectiveBudget.LimitUSD,
			SpendUSD:      raw.Data.EffectiveBudget.SpendUSD,
			RemainingUSD:  raw.Data.EffectiveBudget.RemainingUSD,
			ResetInterval: raw.Data.EffectiveBudget.ResetInterval,
		}
		if raw.Data.EffectiveBudget.ResetsAt != nil {
			b.ResetsAt = *raw.Data.EffectiveBudget.ResetsAt
		}
		info.EffectiveBudget = b
	}
	return info, nil
}
