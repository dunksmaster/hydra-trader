//go:build ignore

// Scale copy trading to 5 leaders x 2 concurrent orders on the shared HL wallet.
// Keeps existing Leviathan bot, recreates Grinder/Money Printer/Copy L4, adds Alpha 6859.
// All bots: fills mode, copy_on_start=false (new opens only), $50 notional, wallet_copy_slots=5.
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

const userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"

type botSpec struct {
	StrategyName string
	TraderName   string
	Leader       string
}

var leaders = []botSpec{
	{StrategyName: "HL Copy Leviathan", TraderName: "🐉 Leviathan", Leader: "0x0ad9e656d9e6211d0ea1c5462342e1fc94cc4cbf"},
	{StrategyName: "HL Copy Grinder", TraderName: "Grinder", Leader: "0xdebbea84972174f44778a00521b1b5faa663abbb"},
	{StrategyName: "HL Copy Money Printer", TraderName: "Money Printer", Leader: "0x8a0cd16a004e21e04936a0a01c6f9a49ff937914"},
	{StrategyName: "HL Copy L4", TraderName: "Copy L4", Leader: "0x6a02aedceac5a6813d960e4dae1910d9c458e77c"},
	{StrategyName: "HL Copy Alpha 6859", TraderName: "Alpha 6859", Leader: "0x6859da14835424957a1e6b397d8026b1d9ff7e1e"},
}

func copyCfg(leader string) map[string]any {
	return map[string]any{
		"leader_address":         leader,
		"copy_mode":              "fills",
		"size_mode":              "fixed_notional",
		"notional_usd":           50,
		"min_notional_usd":       12,
		"max_notional_pct":       55,
		"max_leverage":           10,
		"max_positions":          2,
		"wallet_copy_slots":      5,
		"exit_mode":              "leader_plus_stop",
		"safety_stop_pct":        15,
		"symbol_blocklist":       []string{},
		"reconcile_interval_sec": 60,
		"copy_on_start":          false,
		"min_leader_fill_usd":    10,
		"dry_run":                false,
		"inverse":                false,
	}
}

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run: railway run -- go run ./scripts/ops_scale_five_leaders.go")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	// Backdate iat/nbf: local clock runs ahead of the server, plain GenerateJWT 401s.
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID,
		Email:  "scale-five@local",
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

	trBody, st := get("/api/my-traders")
	if st >= 300 || st == 0 {
		panic(fmt.Sprintf("my-traders status=%d body=%s", st, string(trBody)))
	}
	var trList []map[string]any
	_ = json.Unmarshal(trBody, &trList)

	// Existing traders by name; template model/exchange from Leviathan
	byName := map[string]map[string]any{}
	aiModel, exchangeID := "", ""
	for _, tr := range trList {
		name := fmt.Sprint(tr["trader_name"])
		byName[name] = tr
		if strings.Contains(name, "Leviathan") {
			id := firstStr(tr, "trader_id", "id")
			cfgBody, cfgSt := get("/api/traders/" + id + "/config")
			if cfgSt > 0 && cfgSt < 300 {
				var cfg map[string]any
				_ = json.Unmarshal(cfgBody, &cfg)
				aiModel = firstStr(cfg, "ai_model_id", "ai_model")
				exchangeID = fmt.Sprint(cfg["exchange_id"])
			}
		}
	}
	if aiModel == "" || exchangeID == "" || exchangeID == "<nil>" {
		panic("could not resolve ai_model_id/exchange_id from Leviathan trader")
	}
	fmt.Printf("template model=%s exchange=%s\n", aiModel, exchangeID)

	// Existing strategies by name
	stBody, stSt := get("/api/strategies")
	strategyByName := map[string]string{}
	if stSt > 0 && stSt < 300 {
		var arr []map[string]any
		if json.Unmarshal(stBody, &arr) == nil {
			for _, s := range arr {
				strategyByName[fmt.Sprint(s["name"])] = fmt.Sprint(s["id"])
			}
		} else {
			var wrap map[string]any
			if json.Unmarshal(stBody, &wrap) == nil {
				if items, ok := wrap["strategies"].([]any); ok {
					for _, it := range items {
						if m, ok := it.(map[string]any); ok {
							strategyByName[fmt.Sprint(m["name"])] = fmt.Sprint(m["id"])
						}
					}
				}
			}
		}
	}

	for _, spec := range leaders {
		fmt.Printf("\n=== %s -> %s ===\n", spec.TraderName, spec.Leader)

		// Strategy: update existing or create
		strategyID := strategyByName[spec.StrategyName]
		strategyPayload := map[string]any{
			"config": map[string]any{
				"strategy_type": "copy_trading",
				"language":      "en",
				"copy_config":   copyCfg(spec.Leader),
			},
		}
		if strategyID != "" && strategyID != "<nil>" {
			body, status, err := jsonReq(http.MethodPut, "/api/strategies/"+strategyID, strategyPayload)
			if err != nil || status >= 300 {
				panic(fmt.Sprintf("strategy update %s: status=%d err=%v body=%s", strategyID, status, err, string(body)))
			}
			fmt.Printf("strategy updated %s\n", strategyID)
		} else {
			createPayload := map[string]any{
				"name": spec.StrategyName,
				"config": map[string]any{
					"strategy_type": "copy_trading",
					"language":      "en",
					"copy_config":   copyCfg(spec.Leader),
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

		// Trader: create if missing; for existing traders skip PUT (restart path can wedge
		// on the live WS watcher stop). Strategy link is unchanged for existing bots.
		traderPayload := map[string]any{
			"name": spec.TraderName, "ai_model_id": aiModel, "exchange_id": exchangeID,
			"strategy_id": strategyID, "scan_interval_minutes": 5,
			"is_cross_margin": true, "show_in_competition": false,
		}
		var traderID string
		if existing, ok := byName[spec.TraderName]; ok {
			traderID = firstStr(existing, "trader_id", "id")
			fmt.Printf("trader exists %s (running=%v)\n", traderID[:min(40, len(traderID))], existing["is_running"])
		} else {
			var body []byte
			var status int
			var err error
			for attempt := 1; attempt <= 2; attempt++ {
				body, status, err = jsonReq(http.MethodPost, "/api/traders", traderPayload)
				if err == nil && status > 0 && status < 300 {
					break
				}
				fmt.Printf("  trader create attempt %d warn: status=%d err=%v\n", attempt, status, err)
				time.Sleep(10 * time.Second)
			}
			if err != nil || status >= 300 || status == 0 {
				panic(fmt.Sprintf("trader create: status=%d err=%v body=%s", status, err, string(body)))
			}
			var created map[string]any
			_ = json.Unmarshal(body, &created)
			traderID = firstStr(created, "trader_id", "id")
			fmt.Printf("trader created %s\n", traderID[:min(40, len(traderID))])
		}

		// Start only if not already running (avoid stop/restart on live WS watcher)
		running := false
		if existing, ok := byName[spec.TraderName]; ok {
			running = fmt.Sprint(existing["is_running"]) == "true"
		}
		if running {
			fmt.Println("already running — leaving as-is")
			continue
		}
		body, status, err := jsonReq(http.MethodPost, "/api/traders/"+traderID+"/start", nil)
		if err != nil {
			fmt.Printf("  start warn: %v\n", err)
		} else {
			fmt.Printf("start status=%d %s\n", status, string(body))
		}
		time.Sleep(2 * time.Second)
	}

	fmt.Println("\nDone — 5 copy bots live (fills mode, new opens only, $50 x 2 positions each, slots=5).")
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
