//go:build ignore

// Unpause Alpha 6859: L2 live copy (new opens), overflow to Crypto BigG, restart fills loop.
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
	userID     = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	alphaName  = "Alpha 6859"
	leaderAddr = "0x6859da14835424957a1e6b397d8026b1d9ff7e1e"
)

func main() {
	base := os.Getenv("NOFX_BASE_URL")
	if base == "" {
		base = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing — run: railway run -- go run ./scripts/ops_unpause_alpha6859.go")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "alpha-unpause@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := client.Do(req)
		if err != nil {
			return nil
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
	_ = json.Unmarshal(get("/api/my-traders"), &traders)

	overflowID := ""
	var alpha map[string]any
	for _, tr := range traders {
		name := strings.ToLower(strings.TrimSpace(fmt.Sprint(tr["trader_name"])))
		if strings.Contains(name, "bigg") {
			overflowID = firstStr(tr, "trader_id", "id")
		}
		if strings.Contains(name, "alpha 6859") {
			alpha = tr
		}
	}
	if alpha == nil {
		panic("Alpha 6859 not found")
	}
	id := firstStr(alpha, "trader_id", "id")
	sid := firstStr(alpha, "strategy_id")
	fmt.Printf("alpha id=%s strategy=%s overflow=%s\n", id, sid, overflowID)

	var st map[string]any
	_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
	cfg, _ := st["config"].(map[string]any)
	cc, _ := cfg["copy_config"].(map[string]any)
	if cc == nil {
		cc = map[string]any{}
	}
	fmt.Printf("BEFORE layer=%v paused=%v overflow=%v slots=%v notional=%v\n",
		cc["copy_layer"], cc["copy_paused"], cc["overflow_enabled"], cc["wallet_copy_slots"], cc["notional_usd"])

	cc["leader_address"] = leaderAddr
	cc["copy_layer"] = 2
	cc["copy_paused"] = false
	cc["loss_streak"] = 0
	cc["pause_loss_streak"] = 5
	cc["dry_run"] = false
	cc["wallet_copy_slots"] = 2
	if overflowID != "" {
		cc["overflow_enabled"] = true
		cc["overflow_trader_id"] = overflowID
		cc["overflow_on_skip"] = []string{"already_open", "max_positions", "margin"}
		cc["overflow_max_positions"] = 10
	}
	cfg["copy_config"] = cc
	cfg["strategy_type"] = "copy_trading"

	code, body := put("/api/strategies/"+sid, map[string]any{"config": cfg})
	fmt.Printf("strategy update %d %s\n", code, trunc(body, 120))
	if code == 0 || code >= 300 {
		os.Exit(1)
	}

	if fmt.Sprint(alpha["is_running"]) == "true" {
		sc, sb := post("/api/traders/" + id + "/stop")
		fmt.Printf("stop %d %s\n", sc, trunc(sb, 80))
		time.Sleep(3 * time.Second)
	}
	sc, sb := post("/api/traders/" + id + "/start")
	fmt.Printf("start %d %s\n", sc, trunc(sb, 80))

	fmt.Println("\n=== verify ===")
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := strings.ToLower(fmt.Sprint(tr["trader_name"]))
		if !strings.Contains(name, "alpha 6859") && name != "lucy30" && !strings.Contains(name, "e282") {
			continue
		}
		s := firstStr(tr, "strategy_id")
		var st2 map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+s), &st2)
		c2, _ := st2["config"].(map[string]any)
		cc2, _ := c2["copy_config"].(map[string]any)
		fmt.Printf("  %-18s running=%-5v layer=%v paused=%v overflow=%v\n",
			tr["trader_name"], tr["is_running"], cc2["copy_layer"], cc2["copy_paused"], cc2["overflow_enabled"])
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
