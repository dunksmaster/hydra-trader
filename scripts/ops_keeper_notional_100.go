//go:build ignore

// Raise keeper copy notional to $100 with max_positions=1 (safe on ~$112 shared equity).
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

var keepers = map[string]bool{
	"hyperdash e282":  true,
	"hyperdash b7e0":  true,
	"hyperdash 364a":  true,
	"machibigbrother": true,
}

func main() {
	baseURL := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "notional-100@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 180 * time.Second}

	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b
	}
	putJSON := func(path string, payload any) (int, string) {
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, baseURL+path, bytes.NewReader(raw))
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
		req, _ := http.NewRequest(http.MethodPost, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, strings.TrimSpace(string(b))
	}

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)

	type target struct {
		name, id, sid string
		running       bool
	}
	var targets []target
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		if !keepers[strings.ToLower(strings.TrimSpace(name))] {
			continue
		}
		targets = append(targets, target{
			name: name, id: firstStr(tr, "trader_id", "id"),
			sid:     firstStr(tr, "strategy_id"),
			running: fmt.Sprint(tr["is_running"]) == "true",
		})
	}
	fmt.Printf("keepers found=%d\n", len(targets))

	for _, t := range targets {
		fmt.Printf("\n=== %s ===\n", t.name)
		if t.sid == "" {
			fmt.Println("  no strategy_id — skip")
			continue
		}
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+t.sid), &st)
		cfg, _ := st["config"].(map[string]any)
		if cfg == nil {
			cfg = map[string]any{}
		}
		cc, _ := cfg["copy_config"].(map[string]any)
		if cc == nil {
			fmt.Println("  no copy_config — skip")
			continue
		}
		fmt.Printf("  BEFORE notional=%v max_pos=%v slots=%v paused=%v\n",
			cc["notional_usd"], cc["max_positions"], cc["wallet_copy_slots"], cc["copy_paused"])

		cc["notional_usd"] = 100.0
		cc["min_notional_usd"] = 20.0
		cc["max_positions"] = 1
		cc["wallet_copy_slots"] = 4
		cc["copy_paused"] = false
		cc["size_mode"] = "fixed_notional"
		// Skip leader dust fills that can't clear fee drag at our size
		if toF(cc["min_leader_fill_usd"]) < 25 {
			cc["min_leader_fill_usd"] = 25.0
		}
		cfg["copy_config"] = cc
		cfg["strategy_type"] = "copy_trading"

		code, body := putJSON("/api/strategies/"+t.sid, map[string]any{"config": cfg})
		fmt.Printf("  update status=%d %s\n", code, truncate(body, 140))
		if code == 0 || code >= 300 {
			continue
		}

		// Restart so fills loop reloads notional
		if t.running {
			sc, sb := post("/api/traders/" + t.id + "/stop")
			fmt.Printf("  stop status=%d %s\n", sc, truncate(sb, 80))
			time.Sleep(3 * time.Second)
		}
		sc, sb := post("/api/traders/" + t.id + "/start")
		fmt.Printf("  start status=%d %s\n", sc, truncate(sb, 80))
		time.Sleep(2 * time.Second)
	}

	fmt.Println("\n--- verify ---")
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		if !keepers[strings.ToLower(strings.TrimSpace(name))] {
			continue
		}
		sid := firstStr(tr, "strategy_id")
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		fmt.Printf("%-20s running=%v notional=%v max_pos=%v slots=%v paused=%v\n",
			name, tr["is_running"], cc["notional_usd"], cc["max_positions"], cc["wallet_copy_slots"], cc["copy_paused"])
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
	switch t := v.(type) {
	case float64:
		return t
	case string:
		var f float64
		fmt.Sscanf(t, "%f", &f)
		return f
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
