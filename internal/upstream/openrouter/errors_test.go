package openrouter

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseErrorResponse(t *testing.T) {
	body := `{
	  "error": {
	    "code": 429,
	    "message": "Rate limit exceeded",
	    "metadata": {
	      "error_type": "rate_limit_exceeded",
	      "provider_code": "429",
	      "limit_source": "openrouter_free_limit",
	      "remedy_hint": "Wait and retry"
	    }
	  }
	}`
	resp := &http.Response{
		StatusCode: 429,
		Header:     http.Header{"Retry-After": []string{"12"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	got := ParseErrorResponse(resp)
	if got.Status != 429 || got.Code != 429 {
		t.Fatalf("status/code: %+v", got)
	}
	if got.Message != "Rate limit exceeded" {
		t.Fatalf("message: %q", got.Message)
	}
	if got.ErrorType != "rate_limit_exceeded" || got.RetryAfter != "12" {
		t.Fatalf("meta: %+v", got)
	}
	if got.LimitSource != "openrouter_free_limit" || got.RemedyHint != "Wait and retry" {
		t.Fatalf("limit fields: %+v", got)
	}
	// Body must still be readable for passthrough.
	b, err := io.ReadAll(resp.Body)
	if err != nil || string(b) != body {
		t.Fatalf("body restore failed: %v %q", err, b)
	}
}
