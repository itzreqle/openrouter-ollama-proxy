package model

import "fmt"

// ErrEmptyModel is returned when the model field is missing.
var ErrEmptyModel = fmt.Errorf("model is required")

// UnknownModelError means the model could not be routed.
type UnknownModelError struct {
	Model string
}

func (e *UnknownModelError) Error() string {
	return fmt.Sprintf(
		"unknown model %q: use ollama-claude/<name>, openrouter-claude/<vendor>/<model> (or legacy ollama/ / openrouter/), a vendor/model OpenRouter id, or a local Ollama tag",
		e.Model,
	)
}
