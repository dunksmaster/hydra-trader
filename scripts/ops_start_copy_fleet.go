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

var copyBotNames = []string{
	"leviathan", "grinder", "alpha 6859", "copy l4", "money printer",
	"hyperdash e282", "hyperdash b7e0", "hyperdash 364a", "machibigbrother",
}

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID, Email: "start-copy-fleet@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past), NotBefore: jwt.NewNumericDate(past), Issuer: "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}

	getTraders := func() []map[string]any {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/my-traders", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var traders []map[string]any
		_ = json.Unmarshal(body, &traders)
		return traders
	}

	post := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, strings.TrimSpace(string(body))
	}

	traders := getTraders()
	var targets []map[string]any
	for _, tr := range traders {
		name := strings.ToLower(firstStr(tr, "trader_name", "name"))
		if !isCopyBot(name) {
			continue
		}
		targets = append(targets, tr)
	}
	fmt.Printf("Starting %d copy bots (wallet_copy_slots unchanged at 5)\n\n", len(targets))

	ok, fail, skip := 0, 0, 0
	for _, tr := range targets {
		name := firstStr(tr, "trader_name", "name")
		id := firstStr(tr, "trader_id", "id")
		if fmt.Sprint(tr["is_running"]) == "true" {
			fmt.Printf("  SKIP (already running) %s\n", name)
			skip++
			continue
		}
		code, body := post("/api/traders/" + id + "/start")
		if code >= 200 && code < 300 {
			fmt.Printf("  OK   %s\n", name)
			ok++
		} else {
			fmt.Printf("  FAIL %s — HTTP %d %s\n", name, code, truncate(body, 150))
			fail++
		}
		time.Sleep(2 * time.Second)
	}

	fmt.Printf("\nStart summary: ok=%d skip=%d fail=%d\n\n=== Status after start ===\n", ok, skip, fail)
	time.Sleep(3 * time.Second)
	for _, tr := range getTraders() {
		name := firstStr(tr, "trader_name", "name")
		if !isCopyBot(strings.ToLower(name)) {
			if strings.Contains(strings.ToLower(name), "bigg") {
				fmt.Printf("  %s running=%v (Bitget overflow/AI)\n", name, tr["is_running"])
			}
			continue
		}
		fmt.Printf("  %s running=%v\n", name, tr["is_running"])
	}
}

func isCopyBot(name string) bool {
	for _, want := range copyBotNames {
		if strings.Contains(name, want) {
			return true
		}
	}
	return false
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
