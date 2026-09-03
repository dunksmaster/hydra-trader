//go:build ignore

// Add a Hyperliquid copy leader in watch-only mode (L3 + paused + dry_run).
// The bot starts so the WS watcher streams leader fills, but no trades are placed.
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
	leaderAddr   = "0x020ca66c30bec2c4fe3861a94e4db4a498a35872"
	strategyName = "HL Copy machibigbrother"
	traderName   = "machibigbrother"
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run: railway run -- go run ./scripts/ops_add_watch_leader.go")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID,
		Email:  "watch-leader@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
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

	trBody, trSt := get("/api/my-traders")
	if trSt >= 300 || trSt == 0 {
		panic(fmt.Sprintf("my-traders status=%d body=%s", trSt, string(trBody)))
	}
	var traders []map[string]any
	_ = json.Unmarshal(trBody, &traders)

	aiModel, exchangeID, overflowTraderID := "", "", ""
	for _, tr := range traders {
		name := strings.ToLower(fmt.Sprint(tr["trader_name"]))
		id := firstStr(tr, "trader_id", "id")
		if strings.Contains(name, "leviathan") || strings.Contains(name, "copy l4") {
			cfgBody, cfgSt := get("/api/traders/" + id + "/config")
			if cfgSt > 0 && cfgSt < 300 {
				var cfg map[string]any
				_ = json.Unmarshal(cfgBody, &cfg)
				if aiModel == "" {
					aiModel = firstStr(cfg, "ai_model_id", "ai_model")
					exchangeID = fmt.Sprint(cfg["exchange_id"])
				}
			}
		}
		if strings.Contains(name, "bigg") {
			overflowTraderID = id
		}
		// Idempotent: already exists for this leader
		if id != "" {
			cfgBody, cfgSt := get("/api/traders/" + id + "/config")
			if cfgSt > 0 && cfgSt < 300 {
				var cfg map[string]any
				if json.Unmarshal(cfgBody, &cfg) == nil {
					if stBody, stSt := get("/api/strategies/" + fmt.Sprint(cfg["strategy_id"])); stSt > 0 && stSt < 300 {
						var st map[string]any
						if json.Unmarshal(stBody, &st) == nil {
							if inner, ok := st["config"].(map[string]any); ok {
								if cc, ok := inner["copy_config"].(map[string]any); ok {
									if strings.EqualFold(fmt.Sprint(cc["leader_address"]), leaderAddr) {
										fmt.Printf("Already exists: %s (%s) leader=%s\n", name, id, leaderAddr)
										ensureRunning(jsonReq, id, fmt.Sprint(tr["is_running"]) == "true")
										return
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if aiModel == "" || exchangeID == "" || exchangeID == "<nil>" {
		panic("could not resolve ai_model_id/exchange_id from existing copy trader")
	}
	fmt.Printf("template model=%s exchange=%s overflow=%s\n", aiModel, exchangeID, overflowTraderID)

	stBody, stSt := get("/api/strategies")
	strategyID := ""
	if stSt > 0 && stSt < 300 {
		var arr []map[string]any
		if json.Unmarshal(stBody, &arr) == nil {
			for _, s := range arr {
				if fmt.Sprint(s["name"]) == strategyName {
					strategyID = fmt.Sprint(s["id"])
					break
				}
			}
		}
	}

	watchCfg := map[string]any{
		"leader_address":         leaderAddr,
		"copy_mode":              "fills",
		"size_mode":              "fixed_notional",
		"notional_usd":           50,
		"min_notional_usd":       12,
		"max_notional_pct":       55,
		"max_leverage":           10,
		"max_positions":          1,
		"wallet_copy_slots":      5,
		"exit_mode":              "leader_plus_stop",
		"safety_stop_pct":        15,
		"symbol_blocklist":       []string{},
		"reconcile_interval_sec": 60,
		"copy_on_start":          false,
		"min_leader_fill_usd":    10,
		"dry_run":                true,
		"inverse":                false,
		"copy_layer":             3,
		"copy_paused":            true,
	}
	if overflowTraderID != "" {
		watchCfg["overflow_enabled"] = true
		watchCfg["overflow_trader_id"] = overflowTraderID
		watchCfg["overflow_on_skip"] = []string{"already_open", "max_positions", "margin"}
		watchCfg["overflow_max_positions"] = 10
	}

	strategyPayload := map[string]any{
		"config": map[string]any{
			"strategy_type": "copy_trading",
			"language":      "en",
			"copy_config":   watchCfg,
		},
	}
	if strategyID != "" && strategyID != "<nil>" {
		body, status, err := jsonReq(http.MethodPut, "/api/strategies/"+strategyID, strategyPayload)
		if err != nil || status >= 300 {
			panic(fmt.Sprintf("strategy update: status=%d err=%v body=%s", status, err, string(body)))
		}
		fmt.Printf("strategy updated %s\n", strategyID)
	} else {
		createPayload := map[string]any{
			"name": strategyName,
			"config": map[string]any{
				"strategy_type": "copy_trading",
				"language":      "en",
				"copy_config":   watchCfg,
			},
		}
		body, status, err := jsonReq(http.MethodPost, "/api/strategies", createPayload)
		if err != nil || status >= 300 {
			panic(fmt.Sprintf("strategy create: status=%d err=%v body=%s", status, err, string(body)))
		}
		var created map[string]any
		_ = json.Unmarshal(body, &created)
		strategyID = fmt.Sprint(created["id"])
		fmt.Printf("strategy created %s\n", strategyID)
	}

	traderID := ""
	for _, tr := range traders {
		if fmt.Sprint(tr["trader_name"]) == traderName {
			traderID = firstStr(tr, "trader_id", "id")
			break
		}
	}
	traderPayload := map[string]any{
		"name":                  traderName,
		"ai_model_id":           aiModel,
		"exchange_id":           exchangeID,
		"strategy_id":           strategyID,
		"scan_interval_minutes": 5,
		"is_cross_margin":       true,
		"show_in_competition":   false,
	}
	if traderID != "" {
		body, status, err := jsonReq(http.MethodPut, "/api/traders/"+traderID, traderPayload)
		if err != nil || status >= 300 {
			panic(fmt.Sprintf("trader update: status=%d err=%v body=%s", status, err, string(body)))
		}
		fmt.Printf("trader updated %s\n", traderID)
	} else {
		body, status, err := jsonReq(http.MethodPost, "/api/traders", traderPayload)
		if err != nil || status >= 300 {
			panic(fmt.Sprintf("trader create: status=%d err=%v body=%s", status, err, string(body)))
		}
		var created map[string]any
		_ = json.Unmarshal(body, &created)
		traderID = firstStr(created, "trader_id", "id")
		fmt.Printf("trader created %s\n", traderID)
	}

	running := false
	for _, tr := range traders {
		if firstStr(tr, "trader_id", "id") == traderID {
			running = fmt.Sprint(tr["is_running"]) == "true"
			break
		}
	}
	ensureRunning(jsonReq, traderID, running)

	fmt.Printf("\nDone — %s on waitlist (L3 PAUSED + dry_run). Leader WS watcher active; no trades until you unpause.\n", traderName)
	fmt.Printf("Leader: %s\n", leaderAddr)
}

func ensureRunning(jsonReq func(string, string, any) ([]byte, int, error), traderID string, running bool) {
	if running {
		fmt.Println("already running — watch mode active")
		return
	}
	body, status, err := jsonReq(http.MethodPost, "/api/traders/"+traderID+"/start", nil)
	if err != nil {
		fmt.Printf("start warn: %v\n", err)
		return
	}
	fmt.Printf("start status=%d %s\n", status, string(body))
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
