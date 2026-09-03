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
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	if os.Getenv("JWT_SECRET") == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing")
		os.Exit(1)
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	past := time.Now().Add(-3 * time.Minute)
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{
		UserID: userID,
		Email:  "start-all@local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(6 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(past),
			NotBefore: jwt.NewNumericDate(past),
			Issuer:    "nofxAI",
		},
	}).SignedString(auth.JWTSecret)
	client := &http.Client{Timeout: 120 * time.Second}

	get := func(path string) ([]byte, int) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return body, resp.StatusCode
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

	raw, st := get("/api/my-traders")
	if st != 200 {
		fmt.Printf("my-traders failed status=%d\n", st)
		os.Exit(1)
	}
	var traders []map[string]any
	if err := json.Unmarshal(raw, &traders); err != nil {
		var wrap map[string]any
		_ = json.Unmarshal(raw, &wrap)
		if items, ok := wrap["items"].([]any); ok {
			for _, it := range items {
				if m, ok := it.(map[string]any); ok {
					traders = append(traders, m)
				}
			}
		}
	}
	fmt.Printf("Found %d traders\n\n", len(traders))

	ok, fail, skip := 0, 0, 0
	for _, tr := range traders {
		name := firstStr(tr, "trader_name", "name")
		id := firstStr(tr, "trader_id", "id")
		if id == "" {
			continue
		}
		if fmt.Sprint(tr["is_running"]) == "true" {
			fmt.Printf("  SKIP (already running) %s\n", name)
			skip++
			continue
		}
		code, body := post("/api/traders/" + id + "/start")
		if code >= 200 && code < 300 {
			fmt.Printf("  OK   %s — started\n", name)
			ok++
		} else {
			fmt.Printf("  FAIL %s — HTTP %d %s\n", name, code, truncate(body, 120))
			fail++
		}
		time.Sleep(2 * time.Second)
	}

	fmt.Printf("\nDone: started=%d already_running=%d failed=%d\n", ok, skip, fail)

	raw2, _ := get("/api/my-traders")
	var after []map[string]any
	_ = json.Unmarshal(raw2, &after)
	fmt.Println("\n=== Status after start ===")
	for _, tr := range after {
		fmt.Printf("  %s running=%v\n", firstStr(tr, "trader_name", "name"), tr["is_running"])
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
