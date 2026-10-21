package translate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestToOpenAI(t *testing.T) {
	stream := false
	req := OllamaChatRequest{
		Model: "openrouter/x",
		Messages: []OllamaMessage{
			{Role: "user", Content: "hi"},
		},
		Stream: &stream,
		Options: map[string]any{
			"temperature": 0.2,
			"num_predict": 128.0,
		},
	}
	out, s := ToOpenAI(req, "vendor/model")
	if s {
		t.Fatal("expected non-stream")
	}
	if out.Model != "vendor/model" || len(out.Messages) != 1 {
		t.Fatalf("%+v", out)
	}
	if out.Temperature == nil || *out.Temperature != 0.2 {
		t.Fatalf("temp=%v", out.Temperature)
	}
	if out.MaxTokens == nil || *out.MaxTokens != 128 {
		t.Fatalf("max=%v", out.MaxTokens)
	}
}

func TestFromOpenAIJSON(t *testing.T) {
	in := []byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
	out, err := FromOpenAIJSON(in, "display")
	if err != nil {
		t.Fatal(err)
	}
	var resp OllamaChatResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Done || resp.Message.Content != "hello" || resp.Model != "display" {
		t.Fatalf("%+v", resp)
	}
}

func TestStreamOpenAIToOllama(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	var buf bytes.Buffer
	if err := StreamOpenAIToOllama(strings.NewReader(sse), &buf, "m"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("lines=%v", lines)
	}
	var first OllamaChatResponse
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Message.Content != "Hel" {
		t.Fatalf("%+v", first)
	}
}

func TestRewriteJSONModel(t *testing.T) {
	out, err := RewriteJSONModel([]byte(`{"model":"a","messages":[]}`), "b")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if m["model"] != "b" {
		t.Fatalf("%v", m)
	}
}
