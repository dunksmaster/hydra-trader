package mcp

import "testing"

func TestParseOpenAISSEBodyFullToolCall(t *testing.T) {
	body := []byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"api_request","arguments":"{\"method\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"GET\"}"}}]}}]}

data: [DONE]
`)
	resp, err := parseOpenAISSEBodyFull(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "api_request" {
		t.Fatalf("unexpected tool name: %q", resp.ToolCalls[0].Function.Name)
	}
}

func TestParseResponseBodyFullJSON(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"hello","tool_calls":[]}}]}`)
	client := NewClient().(*Client)
	resp, err := ParseResponseBodyFull(client, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("got %q", resp.Content)
	}
}
