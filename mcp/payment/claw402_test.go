package payment

import (
	"testing"

	"nofx/mcp"
)

func TestOpenAIToolCompat(t *testing.T) {
	tool := mcp.Tool{Type: "function", Function: mcp.FunctionDef{Name: "api_request"}}
	req := &mcp.Request{Model: "gpt-5.6", Tools: []mcp.Tool{tool}}

	body := openAIToolCompat(map[string]any{"model": "gpt-5.6"}, "gpt-5.6", req)
	if body["reasoning_effort"] != "none" {
		t.Fatalf("expected reasoning_effort=none, got %v", body["reasoning_effort"])
	}

	noTools := openAIToolCompat(map[string]any{"model": "gpt-5.6"}, "gpt-5.6", &mcp.Request{Model: "gpt-5.6"})
	if _, ok := noTools["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort should not be set without tools")
	}

	other := openAIToolCompat(map[string]any{"model": "deepseek-v4-flash"}, "deepseek-v4-flash", req)
	if _, ok := other["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort should not be set for non-gpt-5.6 models")
	}
}
