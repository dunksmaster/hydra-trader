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
	userID   = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggID   = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	strategy = "b723efa8-729d-47cd-a71e-99429c639b6a"
)

func main() {
	base := "https://nofx-production-fcd1.up.railway.app"
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "bigg-full@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 90 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b
	}

	fmt.Println("=== AI models ===")
	var models []map[string]any
	_ = json.Unmarshal(get("/api/models"), &models)
	for _, m := range models {
		fmt.Printf("  id=%s provider=%s enabled=%v custom_model=%q custom_url=%q\n",
			m["id"], m["provider"], m["enabled"], m["custom_model_name"], m["custom_api_url"])
	}

	fmt.Println("\n=== Crypto BigG trader config ===")
	fmt.Println(string(get("/api/traders/" + biggID + "/config")))

	fmt.Println("\n=== BigG strategy ===")
	stBody := get("/api/strategies/" + strategy)
	var st map[string]any
	_ = json.Unmarshal(stBody, &st)
	pretty, _ := json.MarshalIndent(st, "", "  ")
	fmt.Println(string(pretty))

	syms := []string{"BTCUSDT", "ETHUSDT", "LITUSDT", "SOLUSDT", "BNBUSDT", "HYPEUSDT"}
	fmt.Println("\n=== Live open orders on Bitget ===")
	for _, sym := range syms {
		raw := get("/api/open-orders?trader_id=" + biggID + "&symbol=" + sym)
		var oo []map[string]any
		if json.Unmarshal(raw, &oo) != nil || len(oo) == 0 {
			continue
		}
		for _, o := range oo {
			fmt.Printf("  %s side=%v type=%v qty=%v price=%v stop=%v status=%v\n",
				sym, o["side"], o["type"], o["quantity"], o["price"], o["stop_price"], o["status"])
		}
	}

	fmt.Println("\n=== Today's opens (ms timestamps) ===")
	var orders []map[string]any
	_ = json.Unmarshal(get("/api/orders?trader_id="+biggID+"&limit=5"), &orders)
	for _, o := range orders {
		ms := toMs(o["filled_at"])
		fmt.Printf("  %s %s %s qty=%s @ %s reasoning=%s\n",
			time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05 UTC"),
			o["symbol"], o["order_action"], o["quantity"], o["exchange_order_id"], truncate(fmt.Sprint(o["reasoning"]), 80))
	}
}

func toMs(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		var f float64
		fmt.Sscan(s, &f)
		return int64(f)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
