//go:build ignore

// machibigbrother: full stop — paused, dry_run, no restart (no trades, no alerts).
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
	userID       = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	machiName    = "machibigbrother"
	leaderAddr   = "0x020ca66c30bec2c4fe3861a94e4db4a498a35872"
)

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "machi-watch@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}
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
	var machi map[string]any
	for _, tr := range traders {
		if strings.Contains(strings.ToLower(fmt.Sprint(tr["trader_name"])), "machibig") {
			machi = tr
			break
		}
	}
	if machi == nil {
		panic("machibigbrother not found")
	}
	id := fmt.Sprint(machi["trader_id"])
	sid := fmt.Sprint(machi["strategy_id"])

	var st map[string]any
	_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
	cfg, _ := st["config"].(map[string]any)
	cc, _ := cfg["copy_config"].(map[string]any)
	if cc == nil {
		cc = map[string]any{}
	}
	cc["leader_address"] = leaderAddr
	cc["copy_layer"] = 3
	cc["copy_paused"] = true
	cc["dry_run"] = true
	cc["copy_on_start"] = false
	cc["overflow_enabled"] = false
	cc["overflow_parallel"] = false
	cc["overflow_trader_id"] = ""
	cfg["copy_config"] = cc

	code, body := put("/api/strategies/"+sid, map[string]any{"config": cfg})
	fmt.Printf("strategy paused %d %s\n", code, trunc(body, 90))

	if fmt.Sprint(machi["is_running"]) == "true" {
		sc, sb := post("/api/traders/" + id + "/stop")
		fmt.Printf("stop machibigbrother %d %s\n", sc, trunc(sb, 90))
	} else {
		fmt.Println("machibigbrother already stopped")
	}

	fmt.Println("\n=== verify ===")
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := strings.ToLower(fmt.Sprint(tr["trader_name"]))
		if !strings.Contains(name, "machi") && !strings.Contains(name, "e282") && name != "lucy30" {
			continue
		}
		s := fmt.Sprint(tr["strategy_id"])
		var st2 map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+s), &st2)
		c2, _ := st2["config"].(map[string]any)
		cc2, _ := c2["copy_config"].(map[string]any)
		fmt.Printf("  %-18s running=%-5v paused=%v dry_run=%v layer=%v\n",
			tr["trader_name"], tr["is_running"], cc2["copy_paused"], cc2["dry_run"], cc2["copy_layer"])
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
