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
		UserID: userID, Email: "trader-kinds@local",
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

	var traders []map[string]any
	_ = json.Unmarshal(get("/api/my-traders"), &traders)
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		if !strings.Contains(strings.ToLower(name), "bigg") && !strings.Contains(strings.ToLower(name), "autopilot") && !strings.Contains(strings.ToLower(name), "e282") {
			continue
		}
		sid := firstStr(tr, "strategy_id")
		exid := firstStr(tr, "exchange_id")
		fmt.Printf("\n=== %s ===\n", name)
		fmt.Printf("running=%v exchange_id=%s\n", tr["is_running"], exid)
		var st map[string]any
		_ = json.Unmarshal(get("/api/strategies/"+sid), &st)
		cfg, _ := st["config"].(map[string]any)
		if cfg == nil {
			continue
		}
		fmt.Printf("strategy_type=%v name=%v\n", cfg["strategy_type"], st["name"])
		if cc, ok := cfg["copy_config"].(map[string]any); ok && cc != nil {
			fmt.Printf("COPY leader=%v layer=%v overflow_to=%v copy_paused=%v\n",
				cc["leader_address"], cc["copy_layer"], cc["overflow_trader_id"], cc["copy_paused"])
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
