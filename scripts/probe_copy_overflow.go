//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"nofx/auth"

	"github.com/golang-jwt/jwt/v5"
)

const userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"

func main() {
	base := os.Getenv("NOFX_BASE_URL")
	if base == "" {
		base = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "probe@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 60 * time.Second}
	get := func(path string) ([]byte, int) {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, _ := client.Do(req)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return body, resp.StatusCode
	}
	body, _ := get("/api/my-traders")
	var traders []map[string]any
	_ = json.Unmarshal(body, &traders)
	seen := map[string]bool{}
	for _, tr := range traders {
		sid := fmt.Sprint(tr["strategy_id"])
		if sid == "" || sid == "<nil>" || seen[sid] {
			continue
		}
		seen[sid] = true
		stBody, st := get("/api/strategies/" + sid)
		if st >= 300 {
			continue
		}
		var stMap map[string]any
		_ = json.Unmarshal(stBody, &stMap)
		cfg, _ := stMap["config"].(map[string]any)
		cc, _ := cfg["copy_config"].(map[string]any)
		if cc == nil {
			fmt.Printf("%s: not copy\n", tr["trader_name"])
			continue
		}
		fmt.Printf("%s leader=%s layer=%v paused=%v dry=%v overflow=%v overflow_id=%v\n",
			tr["trader_name"], cc["leader_address"], cc["copy_layer"], cc["copy_paused"],
			cc["dry_run"], cc["overflow_enabled"], cc["overflow_trader_id"])
	}
	tgBody, _ := get("/api/telegram")
	fmt.Printf("\ntelegram: %s\n", tgBody)
}
