package mcp

import (
	"testing"
	"time"
)

func TestIsNVIDIAThinkingModel(t *testing.T) {
	if !isNVIDIAThinkingModel("nvidia/nemotron-3.5-lightning-30b-a3b", "") {
		t.Fatal("nemotron model")
	}
	if !isNVIDIAThinkingModel("custom", "https://integrate.api.nvidia.com/v1") {
		t.Fatal("nvidia.com host")
	}
	if isNVIDIAThinkingModel("deepseek-chat", "https://api.deepseek.com") {
		t.Fatal("deepseek is not NVIDIA thinking")
	}
}

func TestApplyProviderBodyOverridesCapsNemotronReasoning(t *testing.T) {
	body := map[string]any{"model": "nvidia/nemotron-3.5-lightning-30b-a3b"}
	applyProviderBodyOverrides(body, "nvidia/nemotron-3.5-lightning-30b-a3b", "https://integrate.api.nvidia.com/v1")
	if body["reasoning_budget"] != nvidiaReasoningBudget {
		t.Fatalf("reasoning_budget=%v", body["reasoning_budget"])
	}
	kwargs, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["enable_thinking"] != true {
		t.Fatalf("chat_template_kwargs=%v", body["chat_template_kwargs"])
	}
}

func TestApplyProviderBodyOverridesLeavesOthersAlone(t *testing.T) {
	body := map[string]any{"model": "deepseek-chat"}
	applyProviderBodyOverrides(body, "deepseek-chat", "https://api.deepseek.com")
	if _, ok := body["reasoning_budget"]; ok {
		t.Fatal("must not add reasoning_budget for non-NVIDIA models")
	}
}

func TestSetAPIKeyExtendsTimeoutForNVIDIA(t *testing.T) {
	t.Setenv("AI_TIMEOUT_SECONDS", "")
	client := NewClient().(*Client)
	client.HTTPClient.Timeout = 60 * time.Second
	client.SetAPIKey("k", "https://integrate.api.nvidia.com/v1", "nvidia/nemotron-3.5-lightning-30b-a3b")
	want := nvidiaThinkingTimeout()
	if client.HTTPClient.Timeout < want {
		t.Fatalf("timeout=%v, want >= %v", client.HTTPClient.Timeout, want)
	}
}

func TestNvidiaThinkingTimeoutUsesEnv(t *testing.T) {
	t.Setenv("AI_TIMEOUT_SECONDS", "480")
	if got := nvidiaThinkingTimeout(); got != 480*time.Second {
		t.Fatalf("timeout=%v, want 480s", got)
	}
}
