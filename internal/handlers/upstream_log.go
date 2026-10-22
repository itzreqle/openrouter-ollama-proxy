package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/itzreqle/openrouter-ollama-proxy/internal/upstream/openrouter"
)

// logUpstreamTransportError clarifies client cancel vs real upstream failure.
func logUpstreamTransportError(log *slog.Logger, op string, err error) {
	if log == nil || err == nil {
		return
	}
	switch {
	case errors.Is(err, context.Canceled):
		log.Warn("upstream "+op+" client canceled", "err", err)
	case errors.Is(err, context.DeadlineExceeded):
		log.Warn("upstream "+op+" deadline exceeded", "err", err)
	default:
		log.Error("upstream "+op+" failed", "err", err)
	}
}

// maybeLogOpenRouterError peeks non-2xx OpenRouter bodies and logs structured fields.
// The response body is restored for passthrough.
func maybeLogOpenRouterError(log *slog.Logger, model string, resp *http.Response) {
	if log == nil || resp == nil || resp.StatusCode < 400 {
		return
	}
	apiErr := openrouter.ParseErrorResponse(resp)
	attrs := []any{
		"status", apiErr.Status,
		"model", model,
	}
	if apiErr.Code != 0 {
		attrs = append(attrs, "code", apiErr.Code)
	}
	if apiErr.Message != "" {
		attrs = append(attrs, "message", apiErr.Message)
	}
	if apiErr.LimitSource != "" {
		attrs = append(attrs, "limit_source", apiErr.LimitSource)
	}
	if apiErr.RemedyHint != "" {
		attrs = append(attrs, "remedy", apiErr.RemedyHint)
	}
	if apiErr.ErrorType != "" {
		attrs = append(attrs, "error_type", apiErr.ErrorType)
	}
	if apiErr.ProviderCode != "" {
		attrs = append(attrs, "provider_code", apiErr.ProviderCode)
	}
	if apiErr.Reason != "" {
		attrs = append(attrs, "reason", apiErr.Reason)
	}
	if apiErr.ResetsAt != "" {
		attrs = append(attrs, "resets_at", apiErr.ResetsAt)
	}
	if apiErr.RetryAfter != "" {
		attrs = append(attrs, "retry_after", apiErr.RetryAfter)
	}
	log.Warn("openrouter rejected", attrs...)
}
