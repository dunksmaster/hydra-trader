//go:build ignore

// e282: $200/leg copy, max_notional_pct high enough at 10x, restart to reload config.
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
		UserID: userID, Email: "e282-200@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}

	get := func(path string) map[string]any {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := client.Do(req)
		if err != nil {
			fatal("GET %s: %v", path, err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode >= 300 {
			fatal("GET %s status=%d %s", path, r.StatusCode, string(b))
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		return m
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

	var traders []map[string]any
	req, _ := http.NewRequest(http.MethodGet, base+"/api/my-traders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r, _ := client.Do(req)
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	json.Unmarshal(b, &traders)
	var e282ID, sid string
	for _, tr := range traders {
		if strings.Contains(strings.ToLower(fmt.Sprint(tr["trader_name"])), "e282") {
			e282ID = fmt.Sprint(tr["trader_id"])
			sid = fmt.Sprint(tr["strategy_id"])
			break
		}
	}
	if e282ID == "" {
		fatal("e282 not found")
	}

	st := get("/api/strategies/" + sid)
	cfg, _ := st["config"].(map[string]any)
	cc, _ := cfg["copy_config"].(map[string]any)
	if cc == nil {
		fatal("copy_config missing")
	}

	fmt.Printf("before notional=%v max_pct=%v\n", cc["notional_usd"], cc["max_notional_pct"])

	cc["notional_usd"] = 200.0
	cc["min_notional_usd"] = 20.0
	cc["max_notional_pct"] = 250.0 // allow $200 on ~$100 equity at 10x (~$20 margin)
	cc["max_leverage"] = 10
	cc["size_mode"] = "fixed_notional"
	cc["copy_mode"] = "fills"
	cc["copy_on_start"] = false
	cc["overflow_enabled"] = false
	cc["overflow_parallel"] = false
	cc["overflow_trader_id"] = ""
	cfg["copy_config"] = cc
	cfg["strategy_type"] = "copy_trading"

	code, body := put("/api/strategies/"+sid, map[string]any{
		"name":        st["name"],
		"description": "HL copy e282 $200/leg @10x (fills only, no Bitget overflow)",
		"config":      cfg,
	})
	fmt.Printf("strategy patch %d %s\n", code, trunc(body, 120))
	if code >= 300 {
		fatal("patch failed")
	}

	code, body = post("/api/traders/" + e282ID + "/stop")
	fmt.Printf("stop %d %s\n", code, trunc(body, 80))
	time.Sleep(3 * time.Second)
	code, body = post("/api/traders/" + e282ID + "/start")
	fmt.Printf("start %d %s\n", code, trunc(body, 80))

	st = get("/api/strategies/" + sid)
	cfg, _ = st["config"].(map[string]any)
	cc, _ = cfg["copy_config"].(map[string]any)
	fmt.Printf("\n✅ after notional=%v max_pct=%v leverage=%v\n", cc["notional_usd"], cc["max_notional_pct"], cc["max_leverage"])
	for _, tr := range traders {
		if fmt.Sprint(tr["trader_id"]) == e282ID {
			fmt.Printf("e282 running=%v (restart may take a moment)\n", tr["is_running"])
		}
	}
	fmt.Println("Next leader fill should target ~$200 notional @10x (~$20 margin) after deploy.")
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
