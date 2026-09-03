//go:build ignore

// Activate Crypto BigG: hyper_rank crypto gainers (free), no Claw402 data, overflow off, start trader.
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
	biggTraderID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	biggStrategy = "b723efa8-729d-47cd-a71e-99429c639b6a"
)

func main() {
	mode := "apply"
	if len(os.Args) > 1 {
		mode = strings.ToLower(os.Args[1])
	}

	base := os.Getenv("NOFX_BASE_URL")
	if base == "" {
		base = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	if len(auth.JWTSecret) == 0 {
		fatal("JWT_SECRET missing")
	}
	past := time.Now().Add(-3 * time.Minute)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "bigg-activate@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	if err != nil {
		fatal("jwt: %v", err)
	}

	c := &http.Client{Timeout: 120 * time.Second}
	get := func(path string) any {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := c.Do(req)
		if err != nil {
			fatal("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			fatal("GET %s status=%d body=%s", path, resp.StatusCode, string(body))
		}
		var out any
		json.Unmarshal(body, &out)
		return out
	}
	getMap := func(path string) map[string]any {
		v := get(path)
		m, _ := v.(map[string]any)
		if m == nil {
			fatal("GET %s: expected object", path)
		}
		return m
	}
	put := func(path string, payload any) (int, string) {
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			fatal("PUT %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, strings.TrimSpace(string(b))
	}
	post := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := c.Do(req)
		if err != nil {
			fatal("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, strings.TrimSpace(string(b))
	}

	fmt.Println("=== 1) Verify copy overflow disabled ===")
	var traders []map[string]any
	rawTraders := get("/api/my-traders")
	if arr, ok := rawTraders.([]any); ok {
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				traders = append(traders, m)
			}
		}
	}
	overflowFixed := 0
	for _, tr := range traders {
		sid := str(tr, "strategy_id")
		if sid == "" {
			continue
		}
		st := getMap("/api/strategies/" + sid)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		if cc == nil {
			continue
		}
		on := fmt.Sprint(cc["overflow_enabled"]) == "true"
		tid := strings.TrimSpace(fmt.Sprint(cc["overflow_trader_id"]))
		if on || tid != "" {
			fmt.Printf("  overflow still on: %s enabled=%v target=%q\n", str(tr, "trader_name"), on, tid)
			if mode == "apply" {
				cc["overflow_enabled"] = false
				cc["overflow_trader_id"] = ""
				cfg["copy_config"] = cc
				code, body := put("/api/strategies/"+sid, map[string]any{"config": cfg})
				fmt.Printf("  disabled overflow %s status=%d %s\n", str(tr, "trader_name"), code, trunc(body, 80))
				if code == 200 {
					overflowFixed++
				}
			}
		}
	}
	if overflowFixed == 0 {
		fmt.Println("  all copy overflow paths off")
	}

	fmt.Println("\n=== 2) BigG strategy → hyper_rank crypto gainers (free) ===")
	st := getMap("/api/strategies/" + biggStrategy)
	config, _ := st["config"].(map[string]any)
	ai, _ := config["ai_config"].(map[string]any)
	cs, _ := ai["coin_source"].(map[string]any)
	ind, _ := ai["indicators"].(map[string]any)
	fmt.Printf("  before: source=%v category=%v direction=%v limit=%v\n",
		cs["source_type"], cs["hyper_rank_category"], cs["hyper_rank_direction"], cs["hyper_rank_limit"])

	if mode == "snapshot" {
		return
	}

	cs["source_type"] = "hyper_rank"
	cs["hyper_rank_category"] = "crypto"
	cs["hyper_rank_direction"] = "gainers"
	cs["hyper_rank_limit"] = 10
	cs["use_ai500"] = false
	cs["use_oi_top"] = false
	cs["use_oi_low"] = false
	cs["use_hyper_all"] = false
	cs["use_hyper_main"] = false
	cs["vergex_limit"] = 0
	delete(cs, "static_coins")
	ai["coin_source"] = cs

	ind["enable_quant_data"] = false
	ind["enable_quant_oi"] = false
	ind["enable_oi_ranking"] = false
	ind["enable_netflow_ranking"] = false
	ind["enable_price_ranking"] = false
	ai["indicators"] = ind

	sections, _ := ai["prompt_sections"].(map[string]any)
	if sections != nil {
		sections["entry_standards"] = `# Entry Standards

Trade only symbols from this cycle's Candidate Coins board (Hyperliquid crypto gainers filtered to Bitget USDT perps).

Open only when 15m and 1h agree on direction. Do not chase extended candles; enter on pullback or break-and-retest.

The system sets size to equity x 1.5 notional at 5x. Do not output position_size_usd.`
		sections["role_definition"] = `# You are the NOFX Bitget crypto auto-trader

Trade Bitget USDT perpetuals from this cycle's hyper_rank crypto gainers board. No Claw402/Vergex paid boards. NVIDIA decides; klines are free exchange data.`
		ai["prompt_sections"] = sections
	}

	rc, _ := ai["risk_control"].(map[string]any)
	if rc != nil {
		rc["max_positions"] = 1
		rc["min_confidence"] = 72
		ai["risk_control"] = rc
	}
	config["ai_config"] = ai

	payload := map[string]any{
		"name":        st["name"],
		"description": "Bitget BigG: hyper_rank crypto gainers (free HL board, no Claw402). NVIDIA Nemotron decides; max 1 position.",
		"config":      config,
	}
	code, body := put("/api/strategies/"+biggStrategy, payload)
	fmt.Printf("  strategy patch status=%d %s\n", code, trunc(body, 120))
	if code >= 300 {
		fatal("strategy update failed")
	}

	st = getMap("/api/strategies/" + biggStrategy)
	config, _ = st["config"].(map[string]any)
	ai, _ = config["ai_config"].(map[string]any)
	cs, _ = ai["coin_source"].(map[string]any)
	fmt.Printf("  after: source=%v category=%v direction=%v limit=%v\n",
		cs["source_type"], cs["hyper_rank_category"], cs["hyper_rank_direction"], cs["hyper_rank_limit"])

	fmt.Println("\n=== 3) BigG trader config ===")
	tr := getMap("/api/traders/" + biggTraderID + "/config")
	fmt.Printf("  running=%v model=%v scan=%v min\n", tr["is_running"], tr["ai_model"], tr["scan_interval_minutes"])

	fmt.Println("\n=== 4) Start Crypto BigG ===")
	if fmt.Sprint(tr["is_running"]) == "true" {
		fmt.Println("  already running — reload via stop/start")
		code, body = post("/api/traders/" + biggTraderID + "/stop")
		fmt.Printf("  stop status=%d %s\n", code, trunc(body, 80))
		time.Sleep(2 * time.Second)
	}
	code, body = post("/api/traders/" + biggTraderID + "/start")
	fmt.Printf("  start status=%d %s\n", code, trunc(body, 120))
	if code >= 300 {
		fatal("start failed")
	}

	tr = getMap("/api/traders/" + biggTraderID + "/config")
	fmt.Printf("\n✅ Crypto BigG is_running=%v\n", tr["is_running"])
	fmt.Println("Watch logs for: hyper_rank candidates matched · NVIDIA decision cycle · no Vergex/claw402 on copy bots after next deploy")
}

func str(m map[string]any, keys ...string) string {
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

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
