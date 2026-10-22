package openrouter

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const maxErrorBody = 8192

// APIError is a parsed OpenRouter error envelope.
type APIError struct {
	Status       int
	Code         int
	Message      string
	LimitSource  string
	RemedyHint   string
	ErrorType    string
	ProviderCode string
	Reason       string
	ResetsAt     string
	RetryAfter   string
	RawBody      []byte
}

// ParseErrorResponse peeks a limited error body and restores resp.Body for the caller.
func ParseErrorResponse(resp *http.Response) APIError {
	out := APIError{Status: resp.StatusCode}
	if resp == nil {
		return out
	}
	out.RetryAfter = strings.TrimSpace(resp.Header.Get("Retry-After"))

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	_ = resp.Body.Close()
	out.RawBody = raw
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil || len(raw) == 0 {
		return out
	}

	var envelope struct {
		Error *struct {
			Code     json.Number              `json:"code"`
			Message  string                   `json:"message"`
			Metadata map[string]json.RawMessage `json:"metadata"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Error == nil {
		// Keep a short preview for non-JSON bodies.
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 240 {
			msg = msg[:240] + "…"
		}
		out.Message = msg
		return out
	}

	out.Message = envelope.Error.Message
	if n, err := envelope.Error.Code.Int64(); err == nil {
		out.Code = int(n)
	}
	meta := envelope.Error.Metadata
	out.LimitSource = metaString(meta, "limit_source")
	out.RemedyHint = metaString(meta, "remedy_hint")
	out.ErrorType = metaString(meta, "error_type")
	out.ProviderCode = metaString(meta, "provider_code")
	out.Reason = metaString(meta, "reason")
	out.ResetsAt = metaString(meta, "resets_at")
	return out
}

func metaString(meta map[string]json.RawMessage, key string) string {
	if meta == nil {
		return ""
	}
	raw, ok := meta[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Numbers / other scalars as compact JSON.
	return strings.Trim(string(raw), `"`)
}
