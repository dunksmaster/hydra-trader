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
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "hl-budget@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	c := &http.Client{Timeout: 60 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := c.Do(req)
		if err != nil {
			return nil
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return b
	}

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := strings.ToLower(firstStr(tr, "trader_name"))
		if !strings.Contains(name, "e282") && name != "lucy30" && !strings.Contains(name, "machi") {
			continue
		}
		sid := firstStr(tr, "strategy_id")
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		fmt.Printf("%s running=%v\n", firstStr(tr, "trader_name"), tr["is_running"])
		fmt.Printf("  leader=%s\n", cc["leader_address"])
		fmt.Printf("  notional_usd=%v max_notional_pct=%v max_positions=%v wallet_slots=%v\n",
			cc["notional_usd"], cc["max_notional_pct"], cc["max_positions"], cc["wallet_copy_slots"])
		fmt.Printf("  overflow=%v parallel=%v overflow_notional=%v\n\n",
			cc["overflow_enabled"], cc["overflow_parallel"], cc["overflow_notional_usd"])
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
