package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func looksLikeSSE(body []byte) bool {
	s := strings.TrimSpace(string(body))
	return strings.HasPrefix(s, "data:") || strings.Contains(s, "\ndata: ")
}

func TruncBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}

// ParseResponseBodyFull parses a Claw402/OpenAI response body as JSON or SSE.
// Gateways sometimes return text/event-stream even when stream was not requested.
func ParseResponseBodyFull(hooks ClientHooks, body []byte) (*LLMResponse, error) {
	if r, err := hooks.ParseMCPResponseFull(body); err == nil {
		return r, nil
	}
	if !looksLikeSSE(body) {
		return nil, fmt.Errorf("failed to parse response: invalid character (not JSON or SSE)")
	}
	return parseOpenAISSEBodyFull(body)
}

func parseOpenAISSEBodyFull(body []byte) (*LLMResponse, error) {
	var content, reasoning strings.Builder
	toolCallsByIndex := map[int]*ToolCall{}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Message struct {
					Content          string     `json:"content"`
					ReasoningContent string     `json:"reasoning_content"`
					ToolCalls        []ToolCall `json:"tool_calls"`
				} `json:"message"`
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int              `json:"index"`
						ID       string           `json:"id"`
						Type     string           `json:"type"`
						Function ToolCallFunction `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]

		if len(ch.Message.ToolCalls) > 0 {
			return &LLMResponse{
				Content:          ch.Message.Content,
				ReasoningContent: ch.Message.ReasoningContent,
				ToolCalls:        ch.Message.ToolCalls,
			}, nil
		}
		if ch.Message.Content != "" {
			content.WriteString(ch.Message.Content)
		}
		if ch.Message.ReasoningContent != "" {
			reasoning.WriteString(ch.Message.ReasoningContent)
		}
		if ch.Delta.Content != "" {
			content.WriteString(ch.Delta.Content)
		}
		if ch.Delta.ReasoningContent != "" {
			reasoning.WriteString(ch.Delta.ReasoningContent)
		}
		for _, tc := range ch.Delta.ToolCalls {
			mergeToolCallDelta(toolCallsByIndex, tc.Index, tc.ID, tc.Type, tc.Function)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE scan failed: %w", err)
	}

	toolCalls := sortedToolCalls(toolCallsByIndex)
	if content.Len() == 0 && reasoning.Len() == 0 && len(toolCalls) == 0 {
		return nil, fmt.Errorf("SSE body contained no usable content (len=%d prefix=%q)", len(body), TruncBytes(body, 80))
	}
	return &LLMResponse{
		Content:          content.String(),
		ReasoningContent: reasoning.String(),
		ToolCalls:        toolCalls,
	}, nil
}

func mergeToolCallDelta(m map[int]*ToolCall, index int, id, typ string, fn ToolCallFunction) {
	if _, ok := m[index]; !ok {
		m[index] = &ToolCall{Type: "function"}
	}
	tc := m[index]
	if id != "" {
		tc.ID = id
	}
	if typ != "" {
		tc.Type = typ
	}
	if fn.Name != "" {
		tc.Function.Name = fn.Name
	}
	if fn.Arguments != "" {
		tc.Function.Arguments += fn.Arguments
	}
}

func sortedToolCalls(m map[int]*ToolCall) []ToolCall {
	if len(m) == 0 {
		return nil
	}
	idx := make([]int, 0, len(m))
	for k := range m {
		idx = append(idx, k)
	}
	sort.Ints(idx)
	out := make([]ToolCall, 0, len(idx))
	for _, i := range idx {
		out = append(out, *m[i])
	}
	return out
}
