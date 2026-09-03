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

const biggID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"

func main() {
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: "08ab3fcb-8486-45cf-bd27-0ad35443ff61", Email: "last-trade@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	base := "https://nofx-production-fcd1.up.railway.app"
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			fatal("%v", err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return b
	}

	fmt.Println("=== Last NOFX order (full row) ===")
	var ordWrap map[string]any
	_ = json.Unmarshal(get("/api/orders?trader_id="+biggID+"&limit=1"), &ordWrap)
	orders, _ := ordWrap["orders"].([]any)
	if len(orders) == 0 {
		var flat []any
		_ = json.Unmarshal(get("/api/orders?trader_id="+biggID+"&limit=1"), &flat)
		orders = flat
	}
	if len(orders) > 0 {
		pretty, _ := json.MarshalIndent(orders[0], "", "  ")
		fmt.Println(string(pretty))
	}

	fmt.Println("\n=== Last 3 orders with reasoning/source ===")
	_ = json.Unmarshal(get("/api/orders?trader_id="+biggID+"&limit=20"), &ordWrap)
	orders, _ = ordWrap["orders"].([]any)
	if orders == nil {
		var flat []any
		_ = json.Unmarshal(get("/api/orders?trader_id="+biggID+"&limit=20"), &flat)
		orders = flat
	}
	shown := 0
	for _, raw := range orders {
		o, _ := raw.(map[string]any)
		if o == nil {
			continue
		}
		rsn := strings.TrimSpace(fmt.Sprint(o["reasoning"]))
		if rsn == "" || rsn == "<nil>" {
			rsn = "(no reasoning stored)"
		}
		fmt.Printf("%s | %s %s qty=%v | %s\n",
			ts(o["filled_at"]), o["symbol"], o["order_action"], o["quantity"], truncate(rsn, 100))
		shown++
		if shown >= 3 {
			break
		}
	}

	fmt.Println("\n=== Latest decision record (raw) ===")
	body := get("/api/decisions/latest?trader_id=" + biggID + "&limit=1")
	fmt.Println(string(body))
}

func ts(v any) string {
	var ms int64
	fmt.Sscan(fmt.Sprint(v), &ms)
	if ms > 1e12 {
		return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05 UTC")
	}
	return fmt.Sprint(v)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
