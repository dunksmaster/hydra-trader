//go:build ignore

// Point e282 + lucy30 at two Hyperdash Top-100 wallets (fills-only, $100/leg).
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
	userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	// Hyperdash #1 copy score — majors short book
	leaderE282 = "0x9c062c0575c30a3b7614d0c6ea8de67faab00560"
	// Hyperdash score 92 — BTC short + alts
	leaderLucy = "0x362ad6209a5e904a5569f69884375809c5781d9f"
)

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	if len(auth.JWTSecret) == 0 {
		fatal("JWT_SECRET missing")
	}
	past := time.Now().Add(-3 * time.Minute)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "point-hd-leaders@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	if err != nil {
		fatal("jwt: %v", err)
	}
	client := &http.Client{Timeout: 120 * time.Second}

	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			fatal("GET %s: %v", path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			fatal("GET %s status=%d %s", path, resp.StatusCode, string(b))
		}
		return b
	}
	put := func(path string, payload any) (int, string) {
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, strings.TrimSpace(string(b))
	}
	post := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, strings.TrimSpace(string(b))
	}

	targets := map[string]string{
		"e282":   leaderE282,
		"lucy30": leaderLucy,
	}

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)

	for _, tr := range traders {
		name := strings.ToLower(strings.TrimSpace(fmt.Sprint(tr["trader_name"])))
		var leader string
		var label string
		switch {
		case strings.Contains(name, "e282"):
			leader, label = targets["e282"], "Hyperdash e282"
		case name == "lucy30" || strings.Contains(name, "lucy30"):
			leader, label = targets["lucy30"], "lucy30"
		default:
			continue
		}

		tid := strings.TrimSpace(fmt.Sprint(tr["trader_id"]))
		sid := strings.TrimSpace(fmt.Sprint(tr["strategy_id"]))
		if tid == "" || sid == "" || sid == "<nil>" {
			fatal("%s missing trader/strategy id", label)
		}

		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		if cfg == nil {
			fatal("%s strategy config missing", label)
		}
		cc, _ := cfg["copy_config"].(map[string]any)
		if cc == nil {
			fatal("%s copy_config missing", label)
		}

		before := fmt.Sprint(cc["leader_address"])
		cc["leader_address"] = leader
		cc["copy_mode"] = "fills"
		cc["size_mode"] = "fixed_notional"
		cc["notional_usd"] = 100.0
		cc["min_notional_usd"] = 20.0
		cc["max_notional_pct"] = 100.0
		cc["max_positions"] = 2
		cc["wallet_copy_slots"] = 2
		cc["copy_on_start"] = false // new fills only — do not sync existing bag
		cc["min_leader_fill_usd"] = 25.0
		cc["dry_run"] = false
		cc["copy_paused"] = false
		cc["inverse"] = false
		cfg["copy_config"] = cc
		cfg["strategy_type"] = "copy_trading"

		code, body := put("/api/strategies/"+sid, map[string]any{
			"name":        st["name"],
			"description": fmt.Sprintf("Copy %s fills-only $100/leg (Hyperdash pick)", short(leader)),
			"config":      cfg,
		})
		fmt.Printf("%s strategy patch %d leader %s → %s %s\n", label, code, short(before), short(leader), trunc(body, 100))
		if code >= 300 {
			fatal("strategy update failed for %s", label)
		}

		running := fmt.Sprint(tr["is_running"]) == "true"
		if running {
			code, body = post("/api/traders/" + tid + "/stop")
			fmt.Printf("  stop → %d %s\n", code, trunc(body, 80))
			time.Sleep(2 * time.Second)
		}
		code, body = post("/api/traders/" + tid + "/start")
		fmt.Printf("  start → %d %s\n", code, trunc(body, 80))
	}

	fmt.Println("\n=== verify ===")
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := strings.ToLower(strings.TrimSpace(fmt.Sprint(tr["trader_name"])))
		if !(strings.Contains(name, "e282") || name == "lucy30" || strings.Contains(name, "lucy30")) {
			continue
		}
		sid := strings.TrimSpace(fmt.Sprint(tr["strategy_id"]))
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		fmt.Printf("%s running=%v leader=%s notional=%v copy_on_start=%v paused=%v mode=%v\n",
			tr["trader_name"], tr["is_running"], cc["leader_address"],
			cc["notional_usd"], cc["copy_on_start"], cc["copy_paused"], cc["copy_mode"])
	}
}

func short(a string) string {
	a = strings.ToLower(strings.TrimSpace(a))
	if len(a) < 10 {
		return a
	}
	return a[:6] + "…" + a[len(a)-4:]
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
