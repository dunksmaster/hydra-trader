//go:build ignore

// Set running HL copy bots (e282 + lucy30) to $100 fixed notional per leg.
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

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "copy-100@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 300 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := client.Do(req)
		if err != nil {
			fatal("%v", err)
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
			fatal("%v", err)
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
			fatal("%v", err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, strings.TrimSpace(string(b))
	}

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)

	for _, tr := range traders {
		name := strings.ToLower(firstStr(tr, "trader_name"))
		if !strings.Contains(name, "e282") && name != "lucy30" {
			continue
		}
		if fmt.Sprint(tr["is_running"]) != "true" {
			fmt.Printf("skip %s (not running)\n", firstStr(tr, "trader_name"))
			continue
		}
		id := firstStr(tr, "trader_id")
		sid := firstStr(tr, "strategy_id")
		display := firstStr(tr, "trader_name")

		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		fmt.Printf("\n=== %s ===\n", display)
		fmt.Printf("  before: notional_usd=%v max_positions=%v slots=%v\n",
			cc["notional_usd"], cc["max_positions"], cc["wallet_copy_slots"])

		cc["notional_usd"] = 100.0
		cc["size_mode"] = "fixed_notional"
		cc["min_notional_usd"] = 20.0
		cc["copy_paused"] = false
		cc["overflow_enabled"] = false
		cc["overflow_trader_id"] = ""
		if toF(cc["max_notional_pct"]) > 0 && toF(cc["max_notional_pct"]) < 100 {
			cc["max_notional_pct"] = 100.0
		}
		cfg["copy_config"] = cc
		code, body := put("/api/strategies/"+sid, map[string]any{"config": cfg})
		fmt.Printf("  patch status=%d %s\n", code, trunc(body, 100))
		if code >= 300 {
			continue
		}

		sc, sb := post("/api/traders/" + id + "/stop")
		fmt.Printf("  stop status=%d %s\n", sc, trunc(sb, 60))
		time.Sleep(2 * time.Second)
		sc, sb = post("/api/traders/" + id + "/start")
		fmt.Printf("  start status=%d %s\n", sc, trunc(sb, 60))
	}

	fmt.Println("\n--- verify ---")
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := strings.ToLower(firstStr(tr, "trader_name"))
		if !strings.Contains(name, "e282") && name != "lucy30" {
			continue
		}
		sid := firstStr(tr, "strategy_id")
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		fmt.Printf("%-18s running=%v notional_usd=%v\n",
			firstStr(tr, "trader_name"), tr["is_running"], cc["notional_usd"])
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

func toF(v any) float64 {
	var f float64
	fmt.Sscan(fmt.Sprint(v), &f)
	return f
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
