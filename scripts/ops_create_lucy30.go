//go:build ignore

// Create lucy30 copy bot: clone e282 layout, leader 0x6a02 (Copy L4 wallet), smaller sizing, start only lucy30.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"nofx/auth"

	"github.com/golang-jwt/jwt/v5"
)

const (
	userID       = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	traderName   = "lucy30"
	strategyName = "HL Copy lucy30 6a02"
	leaderAddr   = "0x6a02aedceac5a6813d960e4dae1910d9c458e77c"
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run: railway run -- go run ./scripts/ops_create_lucy30.go")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "lucy30@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 180 * time.Second}

	get := func(path string) ([]byte, int) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return body, resp.StatusCode
	}
	jsonReq := func(method, path string, payload any) ([]byte, int, error) {
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewReader(b)
		}
		req, err := http.NewRequest(method, baseURL+path, body)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		out, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return out, resp.StatusCode, nil
	}

	// Resolve template from Hyperdash e282.
	trBody, _ := get("/api/my-traders")
	var trList []map[string]any
	_ = json.Unmarshal(trBody, &trList)
	var e282 map[string]any
	for _, tr := range trList {
		if strings.Contains(strings.ToLower(fmt.Sprint(tr["trader_name"])), "e282") {
			e282 = tr
			break
		}
	}
	if e282 == nil {
		panic("Hyperdash e282 not found")
	}
	e282ID := firstStr(e282, "trader_id", "id")
	cfgBody, cfgSt := get("/api/traders/" + e282ID + "/config")
	if cfgSt >= 300 {
		panic(fmt.Sprintf("e282 config status=%d", cfgSt))
	}
	var e282Cfg map[string]any
	_ = json.Unmarshal(cfgBody, &e282Cfg)
	aiModel := firstStr(e282Cfg, "ai_model_id", "ai_model")
	exchangeID := fmt.Sprint(e282Cfg["exchange_id"])
	fmt.Printf("template e282 model=%s exchange=%s\n", aiModel, exchangeID)

	// Idempotent: skip if lucy30 already exists.
	lucyID := ""
	for _, tr := range trList {
		name := strings.ToLower(firstStr(tr, "trader_name", "name"))
		if name == "lucy30" || strings.Contains(name, "lucy30") {
			lucyID = firstStr(tr, "trader_id", "id")
			fmt.Printf("lucy30 already exists: %s running=%v\n", lucyID, tr["is_running"])
		}
	}

	// Strategy: smaller than e282 (1000/5 pos) -> 80 notional, 2 max positions, layer 2.
	copyCfg := map[string]any{
		"leader_address":         leaderAddr,
		"copy_mode":              "fills",
		"size_mode":              "fixed_notional",
		"notional_usd":           80,
		"min_notional_usd":       20,
		"max_notional_pct":       45,
		"max_leverage":           10,
		"max_positions":          2,
		"wallet_copy_slots":      5,
		"exit_mode":              "leader_plus_stop",
		"safety_stop_pct":        15,
		"symbol_blocklist":       []string{},
		"reconcile_interval_sec": 60,
		"copy_on_start":          false,
		"min_leader_fill_usd":    25,
		"dry_run":                false,
		"inverse":                false,
		"copy_layer":             2,
		"copy_paused":            false,
		"overflow_enabled":       false,
	}

	strategyID := ""
	stBody, _ := get("/api/strategies")
	var strategies []map[string]any
	_ = json.Unmarshal(stBody, &strategies)
	for _, st := range strategies {
		if fmt.Sprint(st["name"]) == strategyName {
			strategyID = fmt.Sprint(st["id"])
			break
		}
	}
	strategyPayload := map[string]any{
		"config": map[string]any{
			"strategy_type": "copy_trading",
			"language":      "en",
			"copy_config":   copyCfg,
		},
	}
	if strategyID != "" && strategyID != "<nil>" {
		body, status, err := jsonReq(http.MethodPut, "/api/strategies/"+strategyID, strategyPayload)
		if err != nil || status >= 300 {
			panic(fmt.Sprintf("update strategy: %d %v %s", status, err, string(body)))
		}
		fmt.Printf("updated strategy %s (%s)\n", strategyName, strategyID)
	} else {
		createPayload := map[string]any{
			"name":        strategyName,
			"description": "Copy leader 0x6a02 (lucy30) — smaller sizing vs e282",
			"config": map[string]any{
				"strategy_type": "copy_trading",
				"language":      "en",
				"copy_config":   copyCfg,
			},
		}
		body, status, err := jsonReq(http.MethodPost, "/api/strategies", createPayload)
		if err != nil || status >= 300 {
			panic(fmt.Sprintf("create strategy: %d %v %s", status, err, string(body)))
		}
		var created map[string]any
		_ = json.Unmarshal(body, &created)
		strategyID = fmt.Sprint(created["id"])
		fmt.Printf("created strategy %s (%s)\n", strategyName, strategyID)
	}

	customPrompt := "Copy-trading leader wallet: " + leaderAddr

	traderPayload := map[string]any{
		"name":                  traderName,
		"ai_model_id":           aiModel,
		"exchange_id":           exchangeID,
		"strategy_id":           strategyID,
		"scan_interval_minutes": 5,
		"is_cross_margin":       true,
		"show_in_competition":   false,
		"custom_prompt":         customPrompt,
		"override_base_prompt":  false,
	}
	if lucyID == "" {
		body, status, err := jsonReq(http.MethodPost, "/api/traders", traderPayload)
		if err != nil || status >= 300 {
			panic(fmt.Sprintf("create trader: %d %v %s", status, err, string(body)))
		}
		var created map[string]any
		_ = json.Unmarshal(body, &created)
		lucyID = fmt.Sprint(created["trader_id"])
		fmt.Printf("created trader lucy30 (%s)\n", lucyID)
	} else {
		body, status, err := jsonReq(http.MethodPut, "/api/traders/"+lucyID, traderPayload)
		if err != nil || status >= 300 {
			panic(fmt.Sprintf("update trader: %d %v %s", status, err, string(body)))
		}
		fmt.Printf("updated trader lucy30 (%s)\n", lucyID)
	}

	// Start lucy30 only (do not touch other bots).
	body, status, err := jsonReq(http.MethodPost, "/api/traders/"+lucyID+"/start", nil)
	fmt.Printf("start lucy30 status=%d err=%v %s\n", status, err, strings.TrimSpace(string(body)))
	time.Sleep(3 * time.Second)

	// Verify fleet.
	trBody, _ = get("/api/my-traders")
	_ = json.Unmarshal(trBody, &trList)
	fmt.Println("\n=== FLEET ===")
	for _, tr := range trList {
		name := firstStr(tr, "trader_name", "name")
		low := strings.ToLower(name)
		if !strings.Contains(low, "lucy") && !strings.Contains(low, "e282") && !strings.Contains(low, "copy l4") {
			continue
		}
		fmt.Printf("%s running=%v strategy_id=%s\n", name, tr["is_running"], tr["strategy_id"])
	}
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
