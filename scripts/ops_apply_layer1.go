//go:build ignore

// Apply Layer 1 copy priority layout: pause L3 bots, mark L2, create 3 L1 Hyperdash bots.
// Does NOT switch strategy_profile — deploy stays on "current" until /strategy layer1.
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
	CopyLayer    int
	CopyPaused   bool
	CreateNew    bool
}

var layout = []botSpec{
	// L1 — new Hyperdash bots
	{StrategyName: "HL Copy Hyperdash 364a", TraderName: "Hyperdash 364a", Leader: "0x364a45829e8ce2940d8cff911076d8dec40b2e73", CopyLayer: 1, CopyPaused: false, CreateNew: true},
	{StrategyName: "HL Copy Hyperdash b7e0", TraderName: "Hyperdash b7e0", Leader: "0xb7e0b9fbc9479330d70bcc82a7d4325a20e8d1aa", CopyLayer: 1, CopyPaused: false, CreateNew: true},
	{StrategyName: "HL Copy Hyperdash e282", TraderName: "Hyperdash e282", Leader: "0xe2823659be02e0f48a4660e4da008b5e1abfdf29", CopyLayer: 1, CopyPaused: false, CreateNew: true},
	// L1 — promoted from watch-only L3 to live priority
	{StrategyName: "HL Copy machibigbrother", TraderName: "machibigbrother", Leader: "0x020ca66c30bec2c4fe3861a94e4db4a498a35872", CopyLayer: 1, CopyPaused: false},
	// L2 — keep live
	{StrategyName: "HL Copy Leviathan", TraderName: "🐉 Leviathan", Leader: "0x0ad9e656d9e6211d0ea1c5462342e1fc94cc4cbf", CopyLayer: 2, CopyPaused: false},
	{StrategyName: "HL Copy L4", TraderName: "Copy L4", Leader: "0x6a02aedceac5a6813d960e4dae1910d9c458e77c", CopyLayer: 2, CopyPaused: false},
	// L3 — pause (leave in place)
	{StrategyName: "HL Copy Money Printer", TraderName: "Money Printer", Leader: "0x8a0cd16a004e21e04936a0a01c6f9a49ff937914", CopyLayer: 3, CopyPaused: true},
	{StrategyName: "HL Copy Grinder", TraderName: "Grinder", Leader: "0xdebbea84972174f44778a00521b1b5faa663abbb", CopyLayer: 3, CopyPaused: true},
	{StrategyName: "HL Copy Alpha 6859", TraderName: "Alpha 6859", Leader: "0x6859da14835424957a1e6b397d8026b1d9ff7e1e", CopyLayer: 3, CopyPaused: true},
}

func copyCfg(spec botSpec, overflowTraderID string) map[string]any {
	cfg := map[string]any{
		"leader_address":         spec.Leader,
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
		"copy_layer":             spec.CopyLayer,
		"copy_paused":            spec.CopyPaused,
	}
	if overflowTraderID != "" {
		cfg["overflow_enabled"] = true
		cfg["overflow_trader_id"] = overflowTraderID
		cfg["overflow_on_skip"] = []string{"already_open", "max_positions", "margin"}
		cfg["overflow_max_positions"] = 10
	}
	return cfg
}

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run: railway run -- go run ./scripts/ops_apply_layer1.go")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID,
		Email:  "layer1@local",
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

	byName := map[string]map[string]any{}
	strategyIDByTrader := map[string]string{}
	aiModel, exchangeID := "", ""
	overflowTraderID := ""
	for _, tr := range trList {
		name := fmt.Sprint(tr["trader_name"])
		byName[name] = tr
		if sid := firstStr(tr, "strategy_id"); sid != "" {
			strategyIDByTrader[name] = sid
		}
		if strings.Contains(strings.ToLower(name), "leviathan") {
			id := firstStr(tr, "trader_id", "id")
			cfgBody, cfgSt := get("/api/traders/" + id + "/config")
			if cfgSt > 0 && cfgSt < 300 {
				var cfg map[string]any
				_ = json.Unmarshal(cfgBody, &cfg)
				aiModel = firstStr(cfg, "ai_model_id", "ai_model")
				exchangeID = fmt.Sprint(cfg["exchange_id"])
			}
		}
		if strings.Contains(strings.ToLower(name), "bigg") || strings.Contains(strings.ToLower(name), "crypto bigg") {
			overflowTraderID = firstStr(tr, "trader_id", "id")
		}
	}
	if aiModel == "" || exchangeID == "" || exchangeID == "<nil>" {
		panic("could not resolve ai_model_id/exchange_id from Leviathan trader")
	}
	fmt.Printf("template model=%s exchange=%s overflow=%s\n", aiModel, exchangeID, overflowTraderID)

	// Only one non-copy bot may hold a given exchange account, so any AI trader
	// still running on the shared Hyperliquid wallet blocks every copy bot.
	layoutNames := map[string]bool{}
	for _, spec := range layout {
		layoutNames[spec.TraderName] = true
	}
	for _, tr := range trList {
		name := fmt.Sprint(tr["trader_name"])
		if layoutNames[name] || fmt.Sprint(tr["exchange_id"]) != exchangeID {
			continue
		}
		if fmt.Sprint(tr["is_running"]) != "true" {
			continue
		}
		id := firstStr(tr, "trader_id", "id")
		body, status, err := jsonReq(http.MethodPost, "/api/traders/"+id+"/stop", nil)
		fmt.Printf("stopped conflicting non-copy bot %q status=%d err=%v %s\n", name, status, err, strings.TrimSpace(string(body)))
		time.Sleep(2 * time.Second)
	}

	stBody, stSt := get("/api/strategies")
	strategyByName := map[string]string{}
	if stSt > 0 && stSt < 300 {
		var arr []map[string]any
		if json.Unmarshal(stBody, &arr) == nil {
			for _, s := range arr {
				strategyByName[fmt.Sprint(s["name"])] = fmt.Sprint(s["id"])
			}
		}
	}

	for _, spec := range layout {
		fmt.Printf("\n=== %s L%d paused=%v -> %s ===\n", spec.TraderName, spec.CopyLayer, spec.CopyPaused, spec.Leader)

		// /api/strategies only lists the public market, so resolve the real
		// strategy from the trader row first and fall back to name matching.
		strategyID := strategyIDByTrader[spec.TraderName]
		if strategyID == "" || strategyID == "<nil>" {
			strategyID = strategyByName[spec.StrategyName]
		}
		strategyPayload := map[string]any{
			"config": map[string]any{
				"strategy_type": "copy_trading",
				"language":      "en",
				"copy_config":   copyCfg(spec, overflowTraderID),
			},
		}
		if strategyID != "" && strategyID != "<nil>" {
			body, status, err := jsonReq(http.MethodPut, "/api/strategies/"+strategyID, strategyPayload)
			if err != nil || status >= 300 {
				panic(fmt.Sprintf("strategy update %s: status=%d err=%v body=%s", strategyID, status, err, string(body)))
			}
			fmt.Printf("strategy updated %s\n", strategyID)
		} else if spec.CreateNew {
			createPayload := map[string]any{
				"name": spec.StrategyName,
				"config": map[string]any{
					"strategy_type": "copy_trading",
					"language":      "en",
					"copy_config":   copyCfg(spec, overflowTraderID),
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
		} else {
			fmt.Println("strategy missing — skipping trader step")
			continue
		}

		traderPayload := map[string]any{
			"name": spec.TraderName, "ai_model_id": aiModel, "exchange_id": exchangeID,
			"strategy_id": strategyID, "scan_interval_minutes": 5,
			"is_cross_margin": true, "show_in_competition": false,
		}
		var traderID string
		if existing, ok := byName[spec.TraderName]; ok {
			traderID = firstStr(existing, "trader_id", "id")
			fmt.Printf("trader exists %s (running=%v)\n", traderID[:min(40, len(traderID))], existing["is_running"])
		} else if spec.CreateNew {
			body, status, err := jsonReq(http.MethodPost, "/api/traders", traderPayload)
			if err != nil || status >= 300 || status == 0 {
				panic(fmt.Sprintf("trader create: status=%d err=%v body=%s", status, err, string(body)))
			}
			var created map[string]any
			_ = json.Unmarshal(body, &created)
			traderID = firstStr(created, "trader_id", "id")
			fmt.Printf("trader created %s\n", traderID[:min(40, len(traderID))])
		} else {
			fmt.Println("trader missing for existing bot name — check trader_name match")
			continue
		}

		running := false
		if existing, ok := byName[spec.TraderName]; ok {
			running = fmt.Sprint(existing["is_running"]) == "true"
		}
		if running {
			// A bot started before copy_config existed is stuck in the AI loop.
			// Only a restart re-enters the fills loop with the copy config.
			body, status, err := jsonReq(http.MethodPost, "/api/traders/"+traderID+"/stop", nil)
			fmt.Printf("stop status=%d err=%v %s\n", status, err, strings.TrimSpace(string(body)))
			time.Sleep(3 * time.Second)
		}
		body, status, err := jsonReq(http.MethodPost, "/api/traders/"+traderID+"/start", nil)
		if err != nil {
			fmt.Printf("  start warn: %v\n", err)
		} else {
			fmt.Printf("start status=%d %s\n", status, string(body))
		}
		time.Sleep(2 * time.Second)
	}

	fmt.Println("\nDone — layer fields applied. Profile stays 'current' until /strategy layer1 in Telegram.")
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
