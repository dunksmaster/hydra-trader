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
		UserID: userID, Email: "copy-status@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 90 * time.Second}
	get := func(path string) map[string]any {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m
	}

	raw, _ := http.NewRequest(http.MethodGet, baseURL+"/api/my-traders", nil)
	raw.Header.Set("Authorization", "Bearer "+token)
	resp, _ := client.Do(raw)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var traders []map[string]any
	_ = json.Unmarshal(body, &traders)

	fmt.Println("=== Copy bots: running + positions + strategy caps ===")
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		id := firstStr(tr, "trader_id", "id")
		sid := firstStr(tr, "strategy_id")
		if sid == "" {
			continue
		}
		stRaw, _ := http.NewRequest(http.MethodGet, baseURL+"/api/strategies/"+sid, nil)
		stRaw.Header.Set("Authorization", "Bearer "+token)
		stResp, _ := client.Do(stRaw)
		stBody, _ := io.ReadAll(stResp.Body)
		stResp.Body.Close()
		var st map[string]any
		_ = json.Unmarshal(stBody, &st)
		cfg, _ := st["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		if cc == nil {
			continue
		}
		acct := get("/api/account?trader_id=" + id)
		pos := get("/api/positions?trader_id=" + id)
		var positions []any
		if p, ok := pos["positions"].([]any); ok {
			positions = p
		} else if items, ok := pos["items"].([]any); ok {
			positions = items
		}
		fmt.Printf("\n%s running=%v equity=%v pnl=%v\n",
			name, tr["is_running"], acct["total_equity"], acct["total_pnl"])
		fmt.Printf("  leader=%s layer=%v max_pos=%v wallet_slots=%v\n",
			cc["leader_address"], cc["copy_layer"], cc["max_positions"], cc["wallet_copy_slots"])
		for _, p := range positions {
			pm, _ := p.(map[string]any)
			if pm == nil {
				continue
			}
			amt := firstStr(pm, "quantity", "positionAmt", "position_amt")
			if amt == "0" || amt == "" {
				continue
			}
			fmt.Printf("  pos %s %s qty=%s pnl=%v\n", pm["symbol"], pm["side"], amt, pm["unrealized_pnl"])
		}
		if len(positions) == 0 {
			fmt.Println("  (no open positions in API)")
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
