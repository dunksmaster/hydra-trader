package mcp

import (
	"strings"
	"time"
)

// Nemotron / NVIDIA Build thinking models spend a large internal reasoning
// budget before they emit the first completion token. NVIDIA's own sample
// for nvidia/nemotron-3.5-lightning-30b-a3b uses stream=true and
// reasoning_budget=16384. A trading JSON decision does not need that budget,
// and a non-streamed call has to wait for the entire reasoning+answer before
// headers complete — matching the 68-97s calls and "awaiting headers" timeouts.
const (
	nvidiaReasoningBudget    = 2048
	nvidiaThinkingTimeoutMin = 300 * time.Second
)

// nvidiaThinkingTimeout is the HTTP client timeout for NVIDIA thinking models.
// Streaming responses can run for several minutes while reasoning tokens emit;
// AI_TIMEOUT_SECONDS (Railway: 480) must cover the full body read, not just headers.
func nvidiaThinkingTimeout() time.Duration {
	if t := aiTimeoutFromEnv(); t >= nvidiaThinkingTimeoutMin {
		return t
	}
	return nvidiaThinkingTimeoutMin
}

func isNVIDIAThinkingModel(model, baseURL string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	u := strings.ToLower(strings.TrimSpace(baseURL))
	if strings.Contains(m, "nemotron") || strings.Contains(m, "nvidia/") {
		return true
	}
	return strings.Contains(u, "nvidia.com")
}

func applyProviderBodyOverrides(body map[string]any, model, baseURL string) {
	if body == nil || !isNVIDIAThinkingModel(model, baseURL) {
		return
	}
	body["reasoning_budget"] = nvidiaReasoningBudget
	body["chat_template_kwargs"] = map[string]any{
		"enable_thinking": true,
	}
}

func prefersNVIDIAStreaming(model, baseURL string) bool {
	return isNVIDIAThinkingModel(model, baseURL)
}

func ApplyNVIDIAThinkingClientTuning(c *Client, model, baseURL string) {
	if c == nil || c.HTTPClient == nil || !isNVIDIAThinkingModel(model, baseURL) {
		return
	}
	minTimeout := nvidiaThinkingTimeout()
	if c.HTTPClient.Timeout < minTimeout {
		c.HTTPClient.Timeout = minTimeout
	}
}
