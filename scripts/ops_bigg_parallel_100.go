//go:build ignore

// Parallel Bitget mirror: every HL copy open also opens on Crypto BigG at $100.
// Applies to e282 + lucy30 (both leaders). BigG stays overflow venue; AI scans slowed.
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
		UserID: userID, Email: "bigg-parallel@local",
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

	fmt.Println("=== BigG: keep running as Bitget overflow venue ===")
	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	var biggTrader map[string]any
	for _, tr := range traders {
		if strings.Contains(strings.ToLower(firstStr(tr, "trader_name")), "bigg") {
			biggTrader = tr
		}
	}
	if biggTrader == nil {
		panic("Crypto BigG not found")
	}
	if fmt.Sprint(biggTrader["is_running"]) != "true" {
		sc, sb := post("/api/traders/" + biggTraderID + "/start")
		fmt.Printf("  start BigG %d %s\n", sc, trunc(sb, 100))
	}

	fmt.Println("\n=== e282 + lucy30: HL $100 + parallel Bitget $100 ===")
	for _, tr := range traders {
		name := strings.ToLower(firstStr(tr, "trader_name"))
		if !strings.Contains(name, "e282") && name != "lucy30" {
			continue
		}
		sid := firstStr(tr, "strategy_id")
		display := firstStr(tr, "trader_name")

		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		if cc == nil {
			cc = map[string]any{}
		}
		cc["notional_usd"] = 100.0
		cc["max_notional_pct"] = 100.0
		cc["wallet_copy_slots"] = 2
		cc["max_positions"] = 2
		cc["copy_paused"] = false
		cc["overflow_enabled"] = true
		cc["overflow_parallel"] = true
		cc["overflow_trader_id"] = biggTraderID
		cc["overflow_on_skip"] = []string{"already_open", "max_positions", "margin"}
		cc["overflow_notional_usd"] = 100.0
		cc["overflow_max_positions"] = 5
		cfg["copy_config"] = cc

		code, body := put("/api/strategies/"+sid, map[string]any{"config": cfg})
		fmt.Printf("  %s patch %d %s\n", display, code, trunc(body, 90))
	}

	fmt.Println("\n=== Verify ===")
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := strings.ToLower(firstStr(tr, "trader_name"))
		if !strings.Contains(name, "bigg") && !strings.Contains(name, "e282") && name != "lucy30" {
			continue
		}
		sid := firstStr(tr, "strategy_id")
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		parallel := false
		if cc != nil {
			parallel = fmt.Sprint(cc["overflow_parallel"]) == "true"
		}
		fmt.Printf("  %-18s running=%-5v HL=$%v Bitget=$%v parallel=%v\n",
			firstStr(tr, "trader_name"), tr["is_running"],
			val(cc, "notional_usd"), val(cc, "overflow_notional_usd"), parallel)
	}
	fmt.Println("\nNote: parallel mirror needs server build with overflow_parallel support (deploy if not live yet).")
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
