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
		UserID: userID, Email: "copy-restart@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 300 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, _ := client.Do(req)
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return b
	}
	put := func(path string, payload any) (int, string) {
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		r, _ := client.Do(req)
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, strings.TrimSpace(string(b))
	}
	post := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, _ := client.Do(req)
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
		id := firstStr(tr, "trader_id")
		sid := firstStr(tr, "strategy_id")
		display := firstStr(tr, "trader_name")

		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)

		cc["notional_usd"] = 100.0
		cc["size_mode"] = "fixed_notional"
		cc["min_notional_usd"] = 20.0
		cc["max_notional_pct"] = 100.0 // allow full $100 fixed size on ~$104 equity
		cc["wallet_copy_slots"] = 2
		cc["max_positions"] = 2
		cc["copy_paused"] = false
		cc["overflow_enabled"] = true
		cc["overflow_parallel"] = true
		cc["overflow_trader_id"] = biggTraderID
		cfg["copy_config"] = cc

		fmt.Printf("\n=== %s ===\n", display)
		code, body := put("/api/strategies/"+sid, map[string]any{"config": cfg})
		fmt.Printf("  strategy patch %d %s\n", code, trunc(body, 80))

		sc, sb := post("/api/traders/" + id + "/start")
		fmt.Printf("  start %d %s\n", sc, trunc(sb, 80))
		time.Sleep(3 * time.Second)
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
		fmt.Printf("%-18s running=%v notional=%v slots=%v max_pct=%v\n",
			firstStr(tr, "trader_name"), tr["is_running"], cc["notional_usd"], cc["wallet_copy_slots"], cc["max_notional_pct"])
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

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
