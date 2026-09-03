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

const (
	userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
)

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "bigg-who@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 90 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := client.Do(req)
		if err != nil {
			fmt.Println("err", err)
			return nil
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return b
	}

	fmt.Println("=== BigG account ===")
	var acc map[string]any
	_ = json.Unmarshal(get("/api/account?trader_id="+biggID), &acc)
	pretty, _ := json.MarshalIndent(acc, "", "  ")
	fmt.Println(string(pretty))

	fmt.Println("\n=== BigG positions ===")
	fmt.Println(string(get("/api/positions?trader_id=" + biggID)))

	fmt.Println("\n=== Last 25 NOFX orders (BigG) ===")
	var orders []map[string]any
	_ = json.Unmarshal(get("/api/orders?trader_id="+biggID+"&limit=25"), &orders)
	for _, o := range orders {
		ms := toMs(o["filled_at"])
		if ms == 0 {
			ms = toMs(o["created_at"])
		}
		ts := time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05 UTC")
		fmt.Printf("%s %s %s qty=%v reason=%s\n",
			ts, o["symbol"], o["order_action"], o["quantity"], trunc(fmt.Sprint(o["reasoning"]), 160))
	}

	fmt.Println("\n=== Last 10 decisions (BigG) ===")
	var decs []map[string]any
	raw := get("/api/decisions?trader_id=" + biggID + "&limit=10")
	_ = json.Unmarshal(raw, &decs)
	if len(decs) == 0 {
		var wrap map[string]any
		if json.Unmarshal(raw, &wrap) == nil {
			pretty, _ = json.MarshalIndent(wrap, "", "  ")
			fmt.Println(string(pretty)[:min(2000, len(pretty))])
		} else {
			fmt.Println(string(raw)[:min(1500, len(raw))])
		}
	}
	for _, d := range decs {
		fmt.Printf("%v actions=%v success=%v\n", d["timestamp"], trunc(fmt.Sprint(d["actions"]), 200), d["success"])
	}

	fmt.Println("\n=== Live open orders BTC ===")
	fmt.Println(string(get("/api/open-orders?trader_id=" + biggID + "&symbol=BTCUSDT")))
}

func toMs(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	default:
		var f float64
		fmt.Sscan(fmt.Sprint(v), &f)
		return int64(f)
	}
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
