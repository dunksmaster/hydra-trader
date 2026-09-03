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

var keepers = map[string]bool{
	"hyperdash e282": true, "hyperdash b7e0": true,
	"hyperdash 364a": true, "machibigbrother": true,
}

func main() {
	baseURL := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "max-pos-5@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 90 * time.Second}

	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, _ := client.Do(req)
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

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		if !keepers[strings.ToLower(name)] {
			continue
		}
		sid := firstStr(tr, "strategy_id")
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		fmt.Printf("%s BEFORE max_pos=%v slots=%v\n", name, cc["max_positions"], cc["wallet_copy_slots"])
		cc["max_positions"] = 5
		cc["wallet_copy_slots"] = 5 // size against up to 5 concurrent legs
		cc["notional_usd"] = 1000.0
		cc["max_notional_pct"] = 100.0
		cc["max_leverage"] = 10
		cc["copy_paused"] = false
		cfg["copy_config"] = cc
		code, body := putJSON("/api/strategies/"+sid, map[string]any{"config": cfg})
		fmt.Printf("  update %d %s\n", code, truncate(body, 90))
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cc, _ = st["config"].(map[string]any)["copy_config"].(map[string]any)
		fmt.Printf("  AFTER max_pos=%v slots=%v ceiling=%v pct=%v\n",
			cc["max_positions"], cc["wallet_copy_slots"], cc["notional_usd"], cc["max_notional_pct"])
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
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
