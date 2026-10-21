package translate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// OllamaChatRequest is the subset of /api/chat we accept.
type OllamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Stream   *bool           `json:"stream,omitempty"`
	Options  map[string]any  `json:"options,omitempty"`
	Tools    []any           `json:"tools,omitempty"`
	Format   any             `json:"format,omitempty"`
}

// OllamaMessage is a chat message in Ollama format.
type OllamaMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Images    []any  `json:"images,omitempty"`
	ToolCalls []any  `json:"tool_calls,omitempty"`
}

// OpenAIChatRequest is sent to OpenRouter.
type OpenAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Tools       []any           `json:"tools,omitempty"`
}

// OpenAIMessage is a chat message in OpenAI format.
type OpenAIMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCalls  []any  `json:"tool_calls,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// OllamaChatResponse is a non-stream /api/chat response chunk.
type OllamaChatResponse struct {
	Model     string        `json:"model"`
	CreatedAt string        `json:"created_at"`
	Message   OllamaMessage `json:"message"`
	Done      bool          `json:"done"`
	DoneReason string       `json:"done_reason,omitempty"`
}

// ToOpenAI converts an Ollama chat request to OpenAI chat completions.
func ToOpenAI(req OllamaChatRequest, upstreamModel string) (OpenAIChatRequest, bool) {
	stream := true
	if req.Stream != nil {
		stream = *req.Stream
	}

	msgs := make([]OpenAIMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, OpenAIMessage{
			Role:      m.Role,
			Content:   m.Content,
			ToolCalls: m.ToolCalls,
		})
	}

	out := OpenAIChatRequest{
		Model:    upstreamModel,
		Messages: msgs,
		Stream:   stream,
		Tools:    req.Tools,
	}

	if req.Options != nil {
		if v, ok := asFloat(req.Options["temperature"]); ok {
			out.Temperature = &v
		}
		if v, ok := asFloat(req.Options["top_p"]); ok {
			out.TopP = &v
		}
		if v, ok := asInt(req.Options["num_predict"]); ok {
			out.MaxTokens = &v
		}
	}

	return out, stream
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case json.Number:
		i, err := t.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

// OpenAI non-stream response (subset).
type openAIChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []any  `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// FromOpenAIJSON converts a non-streaming OpenAI response to Ollama /api/chat JSON.
func FromOpenAIJSON(body []byte, model string) ([]byte, error) {
	var resp openAIChatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	content := ""
	role := "assistant"
	var toolCalls []any
	doneReason := "stop"
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
		if resp.Choices[0].Message.Role != "" {
			role = resp.Choices[0].Message.Role
		}
		toolCalls = resp.Choices[0].Message.ToolCalls
		if resp.Choices[0].FinishReason != "" {
			doneReason = resp.Choices[0].FinishReason
		}
	}
	out := OllamaChatResponse{
		Model:     model,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Message: OllamaMessage{
			Role:      role,
			Content:   content,
			ToolCalls: toolCalls,
		},
		Done:       true,
		DoneReason: doneReason,
	}
	return json.Marshal(out)
}

// StreamOpenAIToOllama reads OpenAI SSE and writes Ollama NDJSON chat chunks.
func StreamOpenAIToOllama(r io.Reader, w io.Writer, model string) error {
	scanner := bufio.NewScanner(r)
	// large tokens
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	flusher, _ := w.(interface{ Flush() })

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			final := OllamaChatResponse{
				Model:     model,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
				Message:   OllamaMessage{Role: "assistant", Content: ""},
				Done:      true,
			}
			b, _ := json.Marshal(final)
			if _, err := w.Write(append(b, '\n')); err != nil {
				return err
			}
			if flusher != nil {
				flusher.Flush()
			}
			return nil
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					Role      string `json:"role"`
					ToolCalls []any  `json:"tool_calls,omitempty"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		role := delta.Role
		if role == "" {
			role = "assistant"
		}
		done := chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason != ""
		out := OllamaChatResponse{
			Model:     model,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			Message: OllamaMessage{
				Role:      role,
				Content:   delta.Content,
				ToolCalls: delta.ToolCalls,
			},
			Done: done,
		}
		if done && chunk.Choices[0].FinishReason != nil {
			out.DoneReason = *chunk.Choices[0].FinishReason
		}
		b, err := json.Marshal(out)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	// ensure a final done if stream ended without [DONE]
	final := OllamaChatResponse{
		Model:     model,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Message:   OllamaMessage{Role: "assistant", Content: ""},
		Done:      true,
	}
	b, _ := json.Marshal(final)
	_, err := w.Write(append(b, '\n'))
	return err
}

// RewriteJSONModel replaces the "model" field in a JSON object body.
func RewriteJSONModel(body []byte, newModel string) ([]byte, error) {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode body: %w", err)
	}
	m["model"] = newModel
	return json.Marshal(m)
}
