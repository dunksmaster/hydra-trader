//go:build ignore

// Read-only inventory of every trader and strategy, with copy-layer fields.
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
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID,
		Email:  "inventory@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}

	get := func(path string) ([]byte, int) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return body, resp.StatusCode
	}

	trBody, trStatus := get("/api/my-traders")
	var traders []map[string]any
	_ = json.Unmarshal(trBody, &traders)
	fmt.Printf("=== TRADERS (HTTP %d, n=%d) ===\n", trStatus, len(traders))
	for _, tr := range traders {
		fmt.Printf("name=%-24q running=%-5v id=%s strategy=%v exchange=%v model=%v\n",
			fmt.Sprint(tr["trader_name"]), tr["is_running"],
			firstStr(tr, "trader_id", "id"), tr["strategy_id"], tr["exchange_id"], tr["ai_model_id"])
	}

	posBody, posStatus := get("/api/positions")
	fmt.Printf("\n=== FOLLOWER OPEN POSITIONS (HTTP %d) ===\n%s\n", posStatus, string(posBody))

	accBody, accStatus := get("/api/exchanges/account-state")
	fmt.Printf("\n=== EXCHANGE ACCOUNT STATE (HTTP %d) ===\n%s\n", accStatus, string(accBody))

	fmt.Printf("\n=== STRATEGIES (per trader) ===\n")
	for _, tr := range traders {
		sid := strings.TrimSpace(fmt.Sprint(tr["strategy_id"]))
		if sid == "" || sid == "<nil>" {
			continue
		}
		body, status := get("/api/strategies/" + sid)
		var st map[string]any
		_ = json.Unmarshal(body, &st)
		cfg, _ := st["config"].(map[string]any)
		stype := fmt.Sprint(cfg["strategy_type"])
		line := fmt.Sprintf("trader=%-22q strategy=%-30q id=%s http=%d type=%s",
			fmt.Sprint(tr["trader_name"]), fmt.Sprint(st["name"]), sid, status, stype)
		if cc, ok := cfg["copy_config"].(map[string]any); ok && cc != nil {
			line += fmt.Sprintf("\n    leader=%v layer=%v paused=%v dry_run=%v notional=%v slots=%v overflow=%v mode=%v on_start=%v",
				shortAddr(fmt.Sprint(cc["leader_address"])), cc["copy_layer"], cc["copy_paused"],
				cc["dry_run"], cc["notional_usd"], cc["wallet_copy_slots"], cc["overflow_enabled"],
				cc["copy_mode"], cc["copy_on_start"])
		} else if stype == "copy_trading" {
			line += "\n    !! copy_config MISSING !!"
		}
		fmt.Println(line)
	}
}

func shortAddr(a string) string {
	a = strings.TrimSpace(a)
	if len(a) < 12 {
		return a
	}
	return a[:6] + ".." + a[len(a)-4:]
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
