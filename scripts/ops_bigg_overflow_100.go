//go:build ignore

// Start Crypto BigG as Bitget overflow copy venue at $100 for e282 + lucy30.
// AI trading stays off (max_positions=0) — only overflow mirror fills run on Bitget.
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
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	if len(auth.JWTSecret) == 0 {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing")
		os.Exit(1)
	}
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "bigg-overflow@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}

	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return b
	}
	put := func(path string, payload any) (int, string) {
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		r, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, strings.TrimSpace(string(b))
	}
	post := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, strings.TrimSpace(string(b))
	}

	fmt.Println("=== 1) BigG: overflow venue only (AI max_positions=0) ===")
	var biggSt map[string]any
	_ = json.Unmarshal(get("/api/strategies/"+biggStrategy), &biggSt)
	cfg, _ := biggSt["config"].(map[string]any)
	ai, _ := cfg["ai_config"].(map[string]any)
	rc, _ := ai["risk_control"].(map[string]any)
	if rc == nil {
		rc = map[string]any{}
	}
	rc["max_positions"] = 0
	rc["min_confidence"] = 99
	ai["risk_control"] = rc
	cfg["ai_config"] = ai
	code, body := put("/api/strategies/"+biggStrategy, map[string]any{
		"name":        biggSt["name"],
		"description": "Bitget overflow copy venue for HL bots ($100). AI disabled.",
		"config":      cfg,
	})
	fmt.Printf("  strategy patch %d %s\n", code, trunc(body, 100))

	sc, sb := post("/api/traders/" + biggTraderID + "/start")
	fmt.Printf("  start BigG %d %s\n", sc, trunc(sb, 100))

	// Slow AI scans to ~24h — overflow fills still execute immediately.
	var trCfg map[string]any
	_ = json.Unmarshal(get("/api/traders/"+biggTraderID+"/config"), &trCfg)
	putPayload := map[string]any{
		"name":                  firstStr(trCfg, "name", "trader_name"),
		"ai_model_id":           trCfg["ai_model_id"],
		"exchange_id":           trCfg["exchange_id"],
		"strategy_id":           trCfg["strategy_id"],
		"scan_interval_minutes": 1440,
		"is_cross_margin":       trCfg["is_cross_margin"],
	}
	code, body = put("/api/traders/"+biggTraderID, putPayload)
	fmt.Printf("  scan_interval=1440 patch %d %s\n", code, trunc(body, 80))

	fmt.Println("\n=== 2) e282 + lucy30 → overflow to BigG @ $100 ===")
	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := strings.ToLower(firstStr(tr, "trader_name"))
		if !strings.Contains(name, "e282") && name != "lucy30" {
			continue
		}
		sid := firstStr(tr, "strategy_id")
		display := firstStr(tr, "trader_name")

		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		scfg, _ := st["config"].(map[string]any)
		cc, _ := scfg["copy_config"].(map[string]any)
		if cc == nil {
			cc = map[string]any{}
		}
		cc["notional_usd"] = 100.0
		cc["max_notional_pct"] = 100.0
		cc["wallet_copy_slots"] = 2
		cc["max_positions"] = 2
		cc["copy_paused"] = false
		cc["overflow_enabled"] = true
		cc["overflow_trader_id"] = biggTraderID
		cc["overflow_on_skip"] = []string{"already_open", "max_positions", "margin"}
		cc["overflow_notional_usd"] = 100.0
		cc["overflow_max_positions"] = 5
		scfg["copy_config"] = cc

		code, body := put("/api/strategies/"+sid, map[string]any{"config": scfg})
		fmt.Printf("  %s patch %d %s\n", display, code, trunc(body, 80))
	}

	fmt.Println("\n=== 3) Verify ===")
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := strings.ToLower(firstStr(tr, "trader_name"))
		if !strings.Contains(name, "bigg") && !strings.Contains(name, "e282") && name != "lucy30" {
			continue
		}
		sid := firstStr(tr, "strategy_id")
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		scfg, _ := st["config"].(map[string]any)
		cc, _ := scfg["copy_config"].(map[string]any)
		aiCfg, _ := scfg["ai_config"].(map[string]any)
		rc2, _ := aiCfg["risk_control"].(map[string]any)
		overflow := "off"
		if cc != nil && fmt.Sprint(cc["overflow_enabled"]) == "true" {
			overflow = fmt.Sprintf("$%v", cc["overflow_notional_usd"])
		}
		aiMax := ""
		if rc2 != nil {
			aiMax = fmt.Sprint(rc2["max_positions"])
		}
		fmt.Printf("  %-18s running=%-5v HL=$%v overflow=%s ai_max_pos=%s\n",
			firstStr(tr, "trader_name"), tr["is_running"],
			val(cc, "notional_usd"), overflow, aiMax)
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

func val(m map[string]any, k string) string {
	if m == nil {
		return "-"
	}
	return fmt.Sprint(m[k])
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
