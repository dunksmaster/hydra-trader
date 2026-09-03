//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"nofx/auth"
)

const (
	userID          = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggTraderID    = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	autopilotTrader = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502"
	staleModelID    = "08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402"
)

type client struct {
	base  string
	token string
	h     *http.Client
}

func main() {
	base := os.Getenv("NOFX_BASE_URL")
	if base == "" {
		base = "https://nofx-production-fcd1.up.railway.app"
	}
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fatal("JWT_SECRET missing")
	}
	auth.SetJWTSecret(secret)
	token, err := auth.GenerateJWT(userID, "ops@local")
	if err != nil {
		fatal("jwt: %v", err)
	}
	c := &client{base: base, token: token, h: http.DefaultClient}

	// --- 1) Fix stale Autopilot Claw402 model reference ---
	traders := c.getJSON("/api/my-traders")
	traderList, _ := traders.([]any)
	var autopilot map[string]any
	for _, raw := range traderList {
		t, _ := raw.(map[string]any)
		if t == nil {
			continue
		}
		if t["trader_id"] == autopilotTrader || t["trader_name"] == "NOFX Autopilot" {
			autopilot = t
		}
	}
	if autopilot == nil {
		fatal("Autopilot trader not found")
	}
	currentModelID, _ := autopilot["ai_model_id"].(string)
	fmt.Printf("autopilot ai_model_id before=%q\n", currentModelID)

	nvidiaModelID := findNVIDIAModelID(c)
	if nvidiaModelID == "" {
		fatal("no enabled NVIDIA/openai custom model found")
	}
	if currentModelID == staleModelID || strings.Contains(currentModelID, "_claw402") && currentModelID != nvidiaModelID {
		c.fixAutopilotModel(autopilot, nvidiaModelID)
	} else {
		fmt.Printf("autopilot model already ok: %q\n", currentModelID)
	}

	// Strategy board/prompts: use ops_bigg_trend_retune.go (do not overwrite here).

	// --- 2) Start Crypto BigG ---
	status, body := c.post("/api/traders/"+biggTraderID+"/start", nil)
	fmt.Printf("start_bigg status=%d body=%s\n", status, string(body))
	if status >= 300 {
		os.Exit(1)
	}
}

func findNVIDIAModelID(c *client) string {
	modelsRaw := c.getJSON("/api/models")
	models, _ := modelsRaw.([]any)
	for _, raw := range models {
		m, _ := raw.(map[string]any)
		if m == nil {
			continue
		}
		if enabled, _ := m["enabled"].(bool); !enabled {
			continue
		}
		id, _ := m["id"].(string)
		provider, _ := m["provider"].(string)
		customModel, _ := m["custom_model_name"].(string)
		customURL, _ := m["custom_api_url"].(string)
		if strings.Contains(strings.ToLower(customModel), "nemotron") ||
			strings.Contains(strings.ToLower(customURL), "nvidia.com") {
			return id
		}
		if provider == "openai" && strings.Contains(strings.ToLower(customModel), "nvidia") {
			return id
		}
	}
	// fallback: first enabled openai non-claw402
	for _, raw := range models {
		m, _ := raw.(map[string]any)
		if m == nil {
			continue
		}
		if enabled, _ := m["enabled"].(bool); !enabled {
			continue
		}
		provider, _ := m["provider"].(string)
		id, _ := m["id"].(string)
		if provider == "openai" && !strings.Contains(id, "claw402") {
			return id
		}
	}
	return ""
}

func findBigGStrategyID(c *client, traderID string) string {
	// my-traders may include strategy_id
	traders := c.getJSON("/api/my-traders")
	traderList, _ := traders.([]any)
	for _, raw := range traderList {
		t, _ := raw.(map[string]any)
		if t == nil {
			continue
		}
		if t["trader_id"] == traderID {
			if sid, _ := t["strategy_id"].(string); sid != "" {
				return sid
			}
		}
	}
	strategies := c.getJSON("/api/strategies")
	list, _ := strategies.([]any)
	for _, raw := range list {
		s, _ := raw.(map[string]any)
		if s == nil {
			continue
		}
		name, _ := s["name"].(string)
		if strings.Contains(strings.ToLower(name), "claw402") && strings.Contains(strings.ToLower(name), "auto") {
			if id, _ := s["id"].(string); id != "" {
				return id
			}
		}
	}
	return ""
}

func (c *client) fixAutopilotModel(autopilot map[string]any, nvidiaModelID string) {
	payload := map[string]any{
		"name":                  autopilot["trader_name"],
		"ai_model_id":           nvidiaModelID,
		"exchange_id":           autopilot["exchange_id"],
		"strategy_id":           autopilot["strategy_id"],
		"initial_balance":       autopilot["initial_balance"],
		"scan_interval_minutes": autopilot["scan_interval_minutes"],
	}
	if v, ok := autopilot["is_cross_margin"].(bool); ok {
		payload["is_cross_margin"] = v
	}
	if v, ok := autopilot["show_in_competition"].(bool); ok {
		payload["show_in_competition"] = v
	}
	status, body := c.put("/api/traders/"+autopilotTrader, payload)
	fmt.Printf("fix_autopilot_model status=%d body=%s\n", status, string(body))
	if status >= 300 {
		fatal("failed to update autopilot model")
	}
}

func (c *client) switchBigGToHyperRank(strategyID string) {
	strategyRaw := c.getJSON("/api/strategies/" + strategyID)
	strategy, ok := strategyRaw.(map[string]any)
	if !ok {
		fatal("strategy response invalid")
	}
	config, ok := strategy["config"].(map[string]any)
	if !ok {
		fatal("strategy config missing")
	}
	aiConfig, ok := config["ai_config"].(map[string]any)
	if !ok {
		fatal("ai_config missing")
	}
	coinSource, ok := aiConfig["coin_source"].(map[string]any)
	if !ok {
		coinSource = map[string]any{}
		aiConfig["coin_source"] = coinSource
	}
	before, _ := coinSource["source_type"].(string)
	coinSource["source_type"] = "hyper_rank"
	coinSource["hyper_rank_category"] = "crypto"
	coinSource["hyper_rank_direction"] = "gainers"
	coinSource["hyper_rank_limit"] = 5
	coinSource["use_ai500"] = false
	coinSource["use_oi_top"] = false
	coinSource["use_oi_low"] = false
	coinSource["use_hyper_all"] = false
	coinSource["use_hyper_main"] = false
	coinSource["vergex_limit"] = 0

	payload := map[string]any{
		"name":   strategy["name"],
		"config": config,
	}
	status, body := c.put("/api/strategies/"+strategyID, payload)
	fmt.Printf("bigg_strategy source_type %q -> hyper_rank status=%d body=%s\n", before, status, string(body))
	if status >= 300 {
		fatal("failed to update BigG strategy")
	}
}

func (c *client) getJSON(path string) any {
	req, _ := http.NewRequest(http.MethodGet, c.base+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.h.Do(req)
	if err != nil {
		fatal("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		fatal("GET %s status=%d body=%s", path, resp.StatusCode, string(body))
	}
	var out any
	if err := json.Unmarshal(body, &out); err != nil {
		fatal("GET %s json: %v", path, err)
	}
	return out
}

func (c *client) put(path string, payload any) (int, []byte) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPut, c.base+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.h.Do(req)
	if err != nil {
		fatal("PUT %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func (c *client) post(path string, payload any) (int, []byte) {
	var body io.Reader
	if payload != nil {
		data, _ := json.Marshal(payload)
		body = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(http.MethodPost, c.base+path, body)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.h.Do(req)
	if err != nil {
		fatal("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
