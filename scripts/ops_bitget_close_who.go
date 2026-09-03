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
		Email:  "audit@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 60 * time.Second}
	get := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return b
	}

	bigG := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	fmt.Println("=== Crypto BigG closes (all in store) ===")
	raw := get("/api/orders?trader_id=" + bigG + "&limit=500")
	var orders []map[string]any
	_ = json.Unmarshal(raw, &orders)
	for _, o := range orders {
		act := strings.ToLower(fmt.Sprint(o["order_action"]))
		if !strings.Contains(act, "close") {
			continue
		}
		ms := toInt64(o["filled_at"])
		t := time.UnixMilli(ms).UTC().Format("2006-01-02 15:04 UTC")
		fmt.Printf("  %s %s %s qty=%v @ %s\n", t, o["symbol"], act, o["quantity"], o["exchange_order_id"])
	}

	fmt.Println("\n=== All traders running state ===")
	trRaw := get("/api/my-traders")
	var traders []map[string]any
	_ = json.Unmarshal(trRaw, &traders)
	for _, tr := range traders {
		fmt.Printf("  %-22s running=%-5v exchange=%s\n", tr["trader_name"], tr["is_running"], tr["exchange"])
	}

	leader := "0x020ca66c30bec2c4fe3861a94e4db4a498a35872"
	fmt.Printf("\n=== Leader %s…5872 recent fills (public HL) ===\n", leader[:10])
	body := postHL(`{"type":"userFills","user":"` + leader + `"}`)
	var fills []map[string]any
	_ = json.Unmarshal(body, &fills)
	if len(fills) > 10 {
		fills = fills[:10]
	}
	for _, f := range fills {
		coin := fmt.Sprint(f["coin"])
		dir := fmt.Sprint(f["dir"])
		sz := fmt.Sprint(f["sz"])
		px := fmt.Sprint(f["px"])
		ms := toInt64(f["time"])
		t := time.UnixMilli(ms).UTC().Format("2006-01-02 15:04 UTC")
		fmt.Printf("  %s %s dir=%s sz=%s px=%s\n", t, coin, dir, sz, px)
	}
}

func postHL(payload string) []byte {
	req, _ := http.NewRequest(http.MethodPost, "https://api.hyperliquid.xyz/info", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return b
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	default:
		return 0
	}
}
