//go:build ignore

package main

import (
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
	baseURL := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID,
		Email:  "verify-overflow@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 45 * time.Second}
	get := func(path string) map[string]any {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("ERR", path, err)
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var m map[string]any
		if json.Unmarshal(b, &m) == nil && m != nil {
			m["_status"] = float64(resp.StatusCode)
			return m
		}
		var arr []any
		if json.Unmarshal(b, &arr) == nil {
			return map[string]any{"_status": float64(resp.StatusCode), "items": arr}
		}
		return map[string]any{"_status": float64(resp.StatusCode), "raw": string(b)}
	}

	raw := get("/api/my-traders")
	items, _ := raw["items"].([]any)
	fmt.Printf("traders=%d status=%v\n", len(items), raw["_status"])
	for _, it := range items {
		tr, _ := it.(map[string]any)
		name := firstStr(tr, "trader_name", "name")
		low := strings.ToLower(name)
		watch := strings.Contains(low, "leviathan") || strings.Contains(low, "grinder") ||
			strings.Contains(low, "money printer") || strings.Contains(low, "copy l4") ||
			strings.Contains(low, "alpha 6859") || strings.Contains(low, "bigg") ||
			strings.Contains(low, "autopilot")
		if !watch {
			continue
		}
		id := firstStr(tr, "trader_id", "id")
		sid := firstStr(tr, "strategy_id")
		fmt.Printf("%s running=%v\n", name, tr["is_running"])
		if sid != "" {
			st := get("/api/strategies/" + sid)
			if cfg, _ := st["config"].(map[string]any); cfg != nil {
				if cc, _ := cfg["copy_config"].(map[string]any); cc != nil {
					fmt.Printf("  overflow=%v on_start=%v dry=%v notional=%v slots=%v max_pos=%v\n",
						cc["overflow_enabled"], cc["copy_on_start"], cc["dry_run"],
						cc["notional_usd"], cc["wallet_copy_slots"], cc["max_positions"])
					fmt.Printf("  overflow_skips=%v overflow_notional=%v overflow_max=%v\n",
						cc["overflow_on_skip"], cc["overflow_notional_usd"], cc["overflow_max_positions"])
				}
			}
		}
		acct := get("/api/account?trader_id=" + id)
		if acct != nil && acct["_status"] == float64(200) {
			fmt.Printf("  equity=%.2f available=%.2f positions=%v\n",
				num(acct["total_equity"]), num(acct["available_balance"]), acct["position_count"])
		}
		pos := get("/api/positions?trader_id=" + id)
		if posItems, ok := pos["items"].([]any); ok && len(posItems) > 0 {
			for _, p := range posItems {
				pm, _ := p.(map[string]any)
				fmt.Printf("  pos %v %v pnl=%v\n", pm["symbol"], pm["side"], pm["unrealized_pnl"])
			}
		}
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

func num(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	default:
		return 0
	}
}
